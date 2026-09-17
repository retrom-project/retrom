package serverimport

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	model "retrom/internal/model/serverimport"
	"testing"
	"time"
)

type controlMemory struct {
	current          model.ControlSnapshot
	cancel           model.Cancellation
	retry            model.ManualRetry
	writes, reads    int
	readErr, lateErr error
}

func (memory *controlMemory) CommitWrite(_ context.Context, work func(model.ControlScope) error) error {
	if err := work(model.ControlScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateErr
}

func (memory *controlMemory) Current(context.Context, string) (model.ControlSnapshot, error) {
	memory.reads++
	return memory.current, memory.readErr
}

func (memory *controlMemory) Cancel(_ context.Context, plan model.Cancellation) error {
	memory.writes++
	memory.cancel = plan
	memory.current.Summary.State = plan.State
	memory.current.Summary.Version++
	return nil
}

func (memory *controlMemory) Retry(_ context.Context, plan model.ManualRetry) error {
	memory.writes++
	memory.retry = plan
	memory.current.Summary.State = "QUEUED"
	memory.current.Summary.Version++
	return nil
}

func controlFixture() (*Control, *controlMemory) {
	failure := "INTERNAL_ERROR"
	memory := &controlMemory{current: model.ControlSnapshot{Summary: model.Summary{ID: "import", State: "FAILED", Version: 3, JobID: "job", Root: model.RootRef{ID: "root"}, LastErrorCode: &failure, Counts: model.Counts{CatalogItems: 4, NotFound: 1, Imported: 1}}, RootDigest: "root-digest", CatalogDigest: "catalog-digest", JobState: "FAILED", JobVersion: 5, Execution: 2, PendingItems: 2}}
	return NewControl(memory, map[string]string{"root": "root-digest"}, func() time.Time { return time.UnixMilli(100) }), memory
}

func TestQueuedCancellationPreservesTerminalCounts(t *testing.T) {
	service, memory := controlFixture()
	memory.current.Summary.State = "QUEUED"
	memory.current.JobState = "QUEUED"
	_, pending, err := service.Cancel(t.Context(), "import", 3, " stop ", "actor")
	if err != nil {
		t.Fatal(err)
	}
	plan := memory.cancel
	if pending || plan.State != "CANCELLED" || plan.CancelledItems != 2 || plan.CompletedAt == nil || plan.Reason != "stop" || plan.Before.Summary.Counts.NotFound != 1 {
		t.Fatalf("queued cancellation plan: %+v", plan)
	}
}

func TestRunningCancellationWaitsForWorkerAcknowledgement(t *testing.T) {
	service, memory := controlFixture()
	memory.current.Summary.State = "RUNNING"
	memory.current.JobState = "RUNNING"
	_, pending, err := service.Cancel(t.Context(), "import", 3, "stop", "actor")
	if err != nil || !pending || memory.cancel.CompletedAt != nil || memory.cancel.State != "CANCEL_REQUESTED" || memory.cancel.CancelledItems != 0 {
		t.Fatalf("running cancellation: %+v %v", memory.cancel, err)
	}
}

func TestImportControlChecksVersionsAndStorageBeforeWriting(t *testing.T) {
	service, memory := controlFixture()
	if _, err := service.Retry(t.Context(), "import", 2, "actor"); !errors.Is(err, model.ErrNotRetryable) {
		t.Fatalf("stale retry: %v", err)
	}
	memory.current.Summary.State = "QUEUED"
	memory.current.JobState = "QUEUED"
	if _, _, err := service.Cancel(t.Context(), "import", 2, "stop", "actor"); !errors.Is(err, model.ErrNotCancellable) {
		t.Fatalf("stale cancel: %v", err)
	}
	memory.readErr = context.Canceled
	if _, err := service.Retry(t.Context(), "import", 3, "actor"); !errors.Is(err, context.Canceled) {
		t.Fatalf("retry cause: %v", err)
	}
	if memory.writes != 0 {
		t.Fatal("invalid request wrote import state")
	}
}

func TestManualRetryRequiresSameRootAndRetryableFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*controlMemory)
	}{
		{"another active import", func(memory *controlMemory) { memory.current.OtherActive = true }},
		{"different root", func(memory *controlMemory) { memory.current.RootDigest = "changed" }},
		{"terminal success", func(memory *controlMemory) { memory.current.Summary.State = "COMPLETED" }},
		{"new execution", func(memory *controlMemory) { memory.current.JobState = "RUNNING" }},
		{"permanent failure", func(memory *controlMemory) { code := "CATALOG_CHANGED"; memory.current.Summary.LastErrorCode = &code }},
		{"overflow", func(memory *controlMemory) { memory.current.Execution = math.MaxInt64 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory := controlFixture()
			test.change(memory)
			if _, err := service.Retry(t.Context(), "import", 3, "actor"); !errors.Is(err, model.ErrNotRetryable) || memory.writes != 0 {
				t.Fatalf("retry fence: %v", err)
			}
		})
	}
}

func TestManualRetryFreezesNewExecutionInput(t *testing.T) {
	service, memory := controlFixture()
	result, err := service.Retry(t.Context(), "import", 3, "actor")
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Kind, ExecutionID string
		Inputs            struct {
			ServerImportVersion                     int64
			RootConfigDigest, CatalogSnapshotDigest string
		}
	}
	if err := json.Unmarshal(memory.retry.Input, &input); err != nil {
		t.Fatal(err)
	}
	if memory.retry.Execution != 3 || input.Kind != "SERVER_BIOS_IMPORT" || input.ExecutionID == "" || input.Inputs.ServerImportVersion != 3 || input.Inputs.RootConfigDigest != "root-digest" || input.Inputs.CatalogSnapshotDigest != "catalog-digest" || result.Version != 4 {
		t.Fatalf("retry input: %+v plan=%+v", input, memory.retry)
	}
}

func TestControlCommitFailureReturnsNoSuccessfulSummary(t *testing.T) {
	service, memory := controlFixture()
	memory.lateErr = context.Canceled
	result, err := service.Retry(t.Context(), "import", 3, "actor")
	if !errors.Is(err, context.Canceled) || result.ID != "" {
		t.Fatalf("late retry: %+v %v", result, err)
	}
	memory.current.Summary.State = "QUEUED"
	memory.current.JobState = "QUEUED"
	result, pending, err := service.Cancel(t.Context(), "import", memory.current.Summary.Version, "stop", "actor")
	if !errors.Is(err, context.Canceled) || result.ID != "" || pending {
		t.Fatalf("late cancel: %+v %v", result, err)
	}
}
