package emulationstationimport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	library "retrom/internal/service/libraryimport"
)

type handoffMemory struct {
	before                                LeaseSnapshot
	review                                ExecutionReview
	draft                                 library.MetadataDraft
	change                                ExecutionReviewCompletion
	metadata                              library.MetadataChange
	readErr, seedErr, writeErr, commitErr error
	seeds, writes, fences                 int
}

func (memory *handoffMemory) WithReviewHandoff(_ context.Context, run func(ReviewHandoffScope) error) error {
	if err := run(ReviewHandoffScope{Read: memory, Write: memory, Metadata: memory}); err != nil {
		return err
	}
	return memory.commitErr
}

func (memory *handoffMemory) Current(context.Context, string) (LeaseSnapshot, bool, error) {
	return memory.before, true, memory.readErr
}

func (memory *handoffMemory) Review(context.Context, string, string) (ExecutionReview, bool, error) {
	return memory.review, true, memory.readErr
}

func (memory *handoffMemory) Fence(context.Context, LeaseSnapshot, int64) error {
	memory.fences++
	return memory.writeErr
}

func (memory *handoffMemory) CompleteReview(_ context.Context, change ExecutionReviewCompletion) error {
	memory.change = change
	memory.writes++
	return memory.writeErr
}

func (memory *handoffMemory) CurrentMetadata(context.Context, string) (library.MetadataDraft, error) {
	return memory.draft, memory.seedErr
}

func (memory *handoffMemory) SaveMetadata(_ context.Context, change library.MetadataChange) error {
	memory.metadata = change
	memory.seeds++
	return memory.seedErr
}

func newHandoffMemory() *handoffMemory {
	return &handoffMemory{
		before: newItemWorkMemory().before.Execution,
		review: ExecutionReview{ItemID: "source", State: "VALIDATING", Version: 2, LibraryJobID: "ordinary-job", LibraryItemID: "ordinary-item", ReservedJobID: "ordinary-job", ReservedItemID: "ordinary-item", MetadataJSON: `{"title":"Frozen title","releaseYear":2027}`, WarningsJSON: `[]`},
		draft:  library.MetadataDraft{Version: 1, MetadataJSON: `{"title":"Original"}`},
	}
}

func handoffMemoryService(memory *handoffMemory) *ReviewHandoff {
	now := func() time.Time { return time.UnixMilli(2000) }
	return NewReviewHandoff(memory, library.NewMetadataSeeder(nil, now), now)
}

func handoffRequest(memory *handoffMemory) ReviewHandoffRequest {
	return ReviewHandoffRequest{
		Execution:     memory.before.Execution,
		ItemID:        memory.review.ItemID,
		LibraryJobID:  memory.review.ReservedJobID,
		LibraryItemID: memory.review.ReservedItemID,
	}
}

func TestReviewHandoffUsesFrozenYearAndAtomicMetadataScope(t *testing.T) {
	memory := newHandoffMemory()
	if err := handoffMemoryService(memory).Complete(t.Context(), handoffRequest(memory)); err != nil {
		t.Fatal(err)
	}
	if memory.seeds != 1 || memory.writes != 1 || memory.fences != 1 || !strings.Contains(
		memory.metadata.MetadataJSON,
		`"releaseYear":2027`,
	) {
		t.Fatalf("seeds=%d writes=%d metadata=%#v", memory.seeds, memory.writes, memory.metadata)
	}
	if memory.change.NowMS != 2000 || memory.change.WarningsJSON != "[]" || memory.change.Before.ReleaseYearMax != 2027 {
		t.Fatalf("change=%#v", memory.change)
	}
}

func TestReviewHandoffRejectsChangedAuthorityBeforeMetadata(t *testing.T) {
	for _, kind := range []string{"worker", "lease", "deadline", "ordinary", "state", "version", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			memory := newHandoffMemory()
			request := handoffRequest(memory)
			switch kind {
			case "worker":
				memory.before.WorkerID = "other"
			case "lease":
				memory.before.LeaseUntilMS = 2000
			case "deadline":
				memory.before.DeadlineAtMS = 2000
			case "ordinary":
				request.LibraryItemID = "other"
			case "state":
				memory.review.State = "PENDING"
			case "version":
				memory.review.Version = 0
			case "cancel":
				memory.before.JobState = "CANCEL_REQUESTED"
				memory.before.ImportState = "CANCEL_REQUESTED"
			}
			if err := handoffMemoryService(memory).Complete(
				t.Context(),
				request,
			); !errors.Is(
				err,
				ErrVersionConflict,
			) || memory.seeds != 0 || memory.writes != 0 {
				t.Fatalf("error=%v seeds=%d writes=%d", err, memory.seeds, memory.writes)
			}
		})
	}
}

func TestReviewHandoffPreservesStorageCausesAndReplay(t *testing.T) {
	cause := errors.New("handoff failed")
	for _, stage := range []string{"read", "seed", "write", "commit", "replay"} {
		t.Run(stage, func(t *testing.T) {
			memory := newHandoffMemory()
			switch stage {
			case "read":
				memory.readErr = cause
			case "seed":
				memory.seedErr = cause
			case "write":
				memory.writeErr = cause
			case "commit":
				memory.commitErr = cause
			case "replay":
				memory.review.State = "REVIEW_PENDING"
			}
			err := handoffMemoryService(memory).Complete(t.Context(), handoffRequest(memory))
			if stage == "replay" {
				if err != nil || memory.seeds != 0 || memory.writes != 0 {
					t.Fatalf("replay=%v", err)
				}
				return
			}
			if !errors.Is(err, cause) {
				t.Fatalf("lost cause=%v", err)
			}
		})
	}
}
