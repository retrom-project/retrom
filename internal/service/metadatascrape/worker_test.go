package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/metadatascrape"
)

type workerMemory struct {
	run     model.WorkerRun
	claimed bool
	status  model.WorkerStatus
	outcome model.WorkerOutcome
	initial initialMemory
	writes  int
	claim   model.WorkerClaim
}

func (memory *workerMemory) Run(context.Context, string) (model.WorkerRun, error) {
	return memory.run, nil
}

func (memory *workerMemory) CommitClaim(
	_ context.Context, cmd model.WorkerClaimCommand,
) (model.WorkerClaimResult, error) {
	claim := model.WorkerClaim{
		RunID: cmd.Run.RunID, JobID: cmd.Run.JobID, ExecutionNo: cmd.Run.ExecutionNo,
		WorkerID: cmd.WorkerID, Version: cmd.Run.Version, AttemptCount: cmd.Run.AttemptCount,
		Now: cmd.Now,
	}
	claim.Deadline = cmd.Run.Deadline
	if claim.Deadline == 0 {
		claim.Deadline = cmd.Now + 3600000
	}
	claim.Terminal = claim.Deadline <= cmd.Now ||
		cmd.Run.MaxAttempts > 0 && cmd.Run.AttemptCount >= cmd.Run.MaxAttempts ||
		cmd.Run.JobState == "CANCEL_REQUESTED"
	memory.claim = claim
	return model.WorkerClaimResult{Claim: claim, Claimed: memory.claimed}, nil
}

func (memory *workerMemory) CommitRefresh(context.Context, model.WorkerRefreshCommand) (bool, error) {
	return true, nil
}

func (memory *workerMemory) CommitSettle(_ context.Context, cmd model.WorkerSettleCommand) (model.WorkerSettleResult, error) {
	var result model.WorkerSettleResult
	memory.outcome = model.WorkerOutcome{
		Claim: cmd.Claim, State: "SUCCEEDED", RunState: "COMPLETED",
		Count: cmd.Count, Now: cmd.Now,
	}
	switch {
	case memory.status.State == "CANCEL_REQUESTED" || memory.status.State == "CANCELLED":
		memory.outcome.State = "CANCELLED"
		memory.outcome.RunState = "CANCELLED"
		if memory.initial.found && memory.initial.item.ItemState == "SCRAPING" {
			change := model.InitialProgress(memory.initial.item, cmd.Now)
			change.ItemState = "REVIEW_PENDING"
			change.ReviewDelta = 1
			change.JobState = "REVIEW_PENDING"
			memory.initial.changes = append(memory.initial.changes, change)
		}
	case memory.status.State != "RUNNING" && memory.status.State != "QUEUED":
		return result, nil
	case cmd.Failed || memory.status.Expired:
		memory.outcome.State = "FAILED"
		memory.outcome.RunState = "FAILED"
		memory.outcome.Code = cmd.Code
		if !cmd.Failed && memory.status.Expired {
			memory.outcome.Code = "METADATA_EXECUTION_EXPIRED"
			result.Expired = true
		}
	}
	memory.writes++
	return result, nil
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
