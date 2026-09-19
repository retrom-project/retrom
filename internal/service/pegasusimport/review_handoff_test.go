package pegasusimport

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	libraryimportmodel "retrom/internal/model/libraryimport"
	model "retrom/internal/model/pegasusimport"
	library "retrom/internal/service/libraryimport"
)

type handoffMemory struct {
	before                                                          model.ReviewHandoffSnapshot
	change                                                          model.ReviewHandoffChange
	draft                                                           libraryimportmodel.MetadataDraft
	seeded                                                          libraryimportmodel.MetadataChange
	readErr, metadataReadErr, metadataSaveErr, finishErr, commitErr error
	writes, seeds                                                   int
}

func (memory *handoffMemory) WithReviewHandoff(_ context.Context, work func(model.ReviewHandoffScope) error) error {
	if err := work(model.ReviewHandoffScope{Records: memory, Metadata: memory}); err != nil {
		return err
	}
	return memory.commitErr
}

func (memory *handoffMemory) CurrentReviewHandoff(context.Context, string) (model.ReviewHandoffSnapshot, error) {
	return memory.before, memory.readErr
}

func (memory *handoffMemory) FinishReviewHandoff(_ context.Context, change model.ReviewHandoffChange) error {
	memory.change = change
	memory.writes++
	return memory.finishErr
}

func (memory *handoffMemory) CurrentMetadata(context.Context, string) (libraryimportmodel.MetadataDraft, error) {
	return memory.draft, memory.metadataReadErr
}

func (memory *handoffMemory) SaveMetadata(_ context.Context, change libraryimportmodel.MetadataChange) error {
	memory.seeded = change
	memory.seeds++
	return memory.metadataSaveErr
}
func handoffClock() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func readyHandoffMemory() *handoffMemory {
	return &handoffMemory{before: model.ReviewHandoffSnapshot{
		Identity: model.ReviewHandoffRequest{ItemID: "item", ImportID: "import", JobID: "job", LibraryJobID: "library-job", LibraryItemID: "library-item", ExecutionNo: 2, Attempt: 3, WorkerID: "worker"},
		State:    "VALIDATING", ImportState: "RUNNING", JobState: "RUNNING", Version: 4, ImportVersion: 8,
		LeaseUntilMS: handoffClock().UnixMilli() + 60_000, DeadlineMS: handoffClock().UnixMilli() + 120_000,
		Metadata: libraryimportmodel.ServerMetadata{Title: "Frozen title"}, Warnings: []map[string]any{{"code": "SOURCE_WARNING", "field": "file"}},
	}, draft: libraryimportmodel.MetadataDraft{Version: 1, MetadataJSON: `{"title":"Original"}`}}
}

func newHandoffMemoryService(memory *handoffMemory) *ReviewHandoff {
	return NewReviewHandoff(memory, library.NewMetadataSeeder(nil, handoffClock), handoffClock)
}

func TestReviewHandoffSeedsFrozenMetadataInTheSameScope(t *testing.T) {
	t.Parallel()
	memory := readyHandoffMemory()
	year := 1900
	memory.before.Metadata.ReleaseYear = &year
	memory.before.Warnings = append(memory.before.Warnings, map[string]any{"code": "FIELD_VALUE_INVALID", "field": "releaseYear"})
	beforeWarnings := append([]map[string]any(nil), memory.before.Warnings...)
	if err := newHandoffMemoryService(memory).Complete(t.Context(), memory.before.Identity); err != nil {
		t.Fatal(err)
	}
	if memory.writes != 1 || memory.seeds != 1 || memory.seeded.ItemID != "library-item" || memory.seeded.SearchText != "frozen title" {
		t.Fatalf("handoff writes=%d seeds=%d metadata=%#v", memory.writes, memory.seeds, memory.seeded)
	}
	if !reflect.DeepEqual(memory.change.Warnings, beforeWarnings) || !reflect.DeepEqual(memory.before.Warnings, beforeWarnings) || memory.change.NowMS != handoffClock().UnixMilli() {
		t.Fatalf("handoff warnings or clock changed: %#v", memory.change)
	}
}

func TestReviewHandoffRejectsStaleSourceAndExecutionBeforeSeeding(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"execution", "attempt", "library identity", "state", "version", "overflow", "parent version", "parent overflow", "parent state", "job state", "lease", "deadline"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			memory := readyHandoffMemory()
			request := memory.before.Identity
			invalidateHandoffMemory(memory, &request, name)
			err := newHandoffMemoryService(memory).Complete(t.Context(), request)
			if !errors.Is(err, model.ErrVersionConflict) || memory.seeds != 0 || memory.writes != 0 {
				t.Fatalf("invalid %s seeded review: %v writes=%d seeds=%d", name, err, memory.writes, memory.seeds)
			}
		})
	}
}

func invalidateHandoffMemory(memory *handoffMemory, request *model.ReviewHandoffRequest, name string) {
	switch name {
	case "execution":
		request.ExecutionNo++
	case "attempt":
		request.Attempt++
	case "library identity":
		request.LibraryItemID = "foreign"
	case "state":
		memory.before.State = "PENDING"
	case "version":
		memory.before.Version = 0
	case "overflow":
		memory.before.Version = math.MaxInt64
	case "parent version":
		memory.before.ImportVersion = 0
	case "parent overflow":
		memory.before.ImportVersion = math.MaxInt64
	case "parent state":
		memory.before.ImportState = "FAILED"
	case "job state":
		memory.before.JobState = "SUCCEEDED"
	case "lease":
		memory.before.LeaseUntilMS = handoffClock().UnixMilli()
	case "deadline":
		memory.before.DeadlineMS = handoffClock().UnixMilli()
	}
}

func TestReviewHandoffPreservesFailuresAndSkipsRepeatedSeeding(t *testing.T) {
	t.Parallel()
	cause := errors.New("handoff persistence failure")
	for _, phase := range []string{"read", "metadata read", "metadata write", "finish", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			memory := readyHandoffMemory()
			switch phase {
			case "read":
				memory.readErr = cause
			case "metadata read":
				memory.metadataReadErr = cause
			case "metadata write":
				memory.metadataSaveErr = cause
			case "finish":
				memory.finishErr = cause
			case "commit":
				memory.commitErr = cause
			}
			if err := newHandoffMemoryService(memory).Complete(t.Context(), memory.before.Identity); !errors.Is(err, cause) {
				t.Fatalf("%s lost cause: %v", phase, err)
			}
		})
	}
	memory := readyHandoffMemory()
	memory.before.State = "REVIEW_PENDING"
	memory.before.JobState = "SUCCEEDED"
	if err := newHandoffMemoryService(memory).Complete(t.Context(), memory.before.Identity); err != nil || memory.seeds != 0 || memory.writes != 0 {
		t.Fatalf("repeated handoff: %v writes=%d seeds=%d", err, memory.writes, memory.seeds)
	}
}

func TestReviewHandoffCompletesAlreadyCreatedReviewDuringCancellation(t *testing.T) {
	t.Parallel()
	memory := readyHandoffMemory()
	memory.before.ImportState = "CANCEL_REQUESTED"
	memory.before.JobState = "CANCEL_REQUESTED"
	if err := newHandoffMemoryService(memory).Complete(t.Context(), memory.before.Identity); err != nil || memory.writes != 1 {
		t.Fatalf("cancelled in-flight handoff lost review: %v", err)
	}
}
