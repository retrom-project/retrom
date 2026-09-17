package metadatascrape

import (
	"context"
	"errors"
	model "retrom/internal/model/metadatascrape"
	"testing"
	"time"
)

type workerMemory struct {
	run     model.WorkerRun
	claim   model.WorkerClaim
	claimed bool
	status  model.WorkerStatus
	outcome model.WorkerOutcome
	initial initialMemory
	writes  int
}

func (memory *workerMemory) Run(context.Context, string) (model.WorkerRun, error) {
	return memory.run, nil
}

func (memory *workerMemory) CommitWrite(ctx context.Context, work func(model.WorkerScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return work(model.WorkerScope{Leases: memory, Write: memory, Initial: model.InitialReviewScope{Read: &memory.initial, Write: &memory.initial}})
}

func (memory *workerMemory) Claim(_ context.Context, claim model.WorkerClaim) (bool, error) {
	memory.claim = claim
	return memory.claimed, nil
}

func (memory *workerMemory) Refresh(context.Context, model.WorkerClaim, int64) (bool, error) {
	return true, nil
}

func (memory *workerMemory) Status(context.Context, model.WorkerClaim, int64) (model.WorkerStatus, error) {
	return memory.status, nil
}

func (memory *workerMemory) Finish(_ context.Context, outcome model.WorkerOutcome) error {
	memory.outcome = outcome
	memory.writes++
	return nil
}

type processFunc func(context.Context, model.WorkerClaim, string) (int, string, error)

func (process processFunc) Process(ctx context.Context, claim model.WorkerClaim, payload string) (int, string, error) {
	return process(ctx, claim, payload)
}

func TestUnclaimedMetadataExecutionDoesNotProcessOrFinish(t *testing.T) {
	memory := &workerMemory{run: model.WorkerRun{RunID: "run", JobID: "job", Provider: "HASHEOUS", State: "RUNNING", JobState: "QUEUED", ExecutionNo: 3}}
	processor := processFunc(func(context.Context, model.WorkerClaim, string) (int, string, error) {
		t.Fatal("unclaimed execution processed")
		return 0, "", nil
	})
	if err := NewWorker(memory, processor, func() time.Time { return time.UnixMilli(100) }).Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	if memory.writes != 0 || memory.claim.WorkerID == "" || memory.claim.ExecutionNo != 3 || memory.claim.Deadline != 3600100 {
		t.Fatalf("unclaimed execution: %+v", memory)
	}
}

func TestCancelledContextStillSettlesOwnedExecutionAndPreservesCause(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	memory := &workerMemory{run: model.WorkerRun{RunID: "run", JobID: "job", Provider: "HASHEOUS", State: "RUNNING", JobState: "QUEUED", ExecutionNo: 1}, claimed: true, status: model.WorkerStatus{State: "RUNNING"}}
	original := errors.New("provider storage failed")
	processor := processFunc(func(context.Context, model.WorkerClaim, string) (int, string, error) {
		cancel()
		return 0, "STORAGE_FAILED", original
	})
	err := NewWorker(memory, processor, time.Now).Run(parent, "run")
	if !errors.Is(err, original) || !errors.Is(err, context.Canceled) || memory.writes != 1 || memory.outcome.State != "FAILED" {
		t.Fatalf("settlement=%+v error=%v", memory.outcome, err)
	}
}

func TestQueuedCancellationReconcilesInitialItemWithoutProcessing(t *testing.T) {
	memory := &workerMemory{run: model.WorkerRun{RunID: "run", JobID: "job", Provider: "HASHEOUS", State: "RUNNING", JobState: "CANCELLED", ExecutionNo: 1}, status: model.WorkerStatus{State: "CANCELLED"}, initial: initialMemory{found: true, item: model.InitialImport{ItemState: "SCRAPING", Running: 1}}}
	processor := processFunc(func(context.Context, model.WorkerClaim, string) (int, string, error) {
		t.Fatal("cancelled execution processed")
		return 0, "", nil
	})
	if err := NewWorker(memory, processor, time.Now).Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	if memory.outcome.State != "CANCELLED" || len(memory.initial.changes) != 1 || memory.initial.changes[0].ItemState != "REVIEW_PENDING" {
		t.Fatalf("cancelled initial review: %+v", memory)
	}
}

func TestExpiredExecutionCannotPublishSuccess(t *testing.T) {
	memory := &workerMemory{status: model.WorkerStatus{State: "RUNNING", Expired: true}}
	err := NewWorker(memory, nil, time.Now).settle(t.Context(), model.WorkerClaim{RunID: "run"}, 1, "", nil)
	if !errors.Is(err, context.DeadlineExceeded) || memory.outcome.State != "FAILED" || memory.outcome.Code != "METADATA_EXECUTION_EXPIRED" {
		t.Fatalf("expired publication: %+v / %v", memory.outcome, err)
	}
}

func (memory *workerMemory) Recoverable(context.Context, int64) ([]string, error) { return nil, nil }

func (memory *workerMemory) Requeue(context.Context, model.WorkerClaim, int64) (bool, error) {
	return true, nil
}
