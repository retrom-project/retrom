package serverimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type outcomeMemory struct {
	counts           map[string]int64
	budget           RetryBudget
	final            FinalOutcome
	retry            AutomaticRetry
	item             ItemOutcome
	writes           int
	access           WorkerAccess
	lateErr, readErr error
}

func (memory *outcomeMemory) CommitWrite(_ context.Context, work func(OutcomeScope) error) error {
	if err := work(OutcomeScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateErr
}

func (*outcomeMemory) Recovery(context.Context, int64) (RecoveryWork, bool, error) {
	return RecoveryWork{}, false, nil
}

func (memory *outcomeMemory) Lock(_ context.Context, _ Work, _ int64, access WorkerAccess) error {
	memory.access = access
	return nil
}

func (memory *outcomeMemory) Counts(context.Context, Work) (map[string]int64, error) {
	return memory.counts, memory.readErr
}

func (memory *outcomeMemory) Budget(context.Context, Work) (RetryBudget, error) {
	return memory.budget, memory.readErr
}

func (memory *outcomeMemory) Item(_ context.Context, plan ItemOutcome) error {
	memory.item = plan
	memory.writes++
	return nil
}

func (memory *outcomeMemory) Final(_ context.Context, plan FinalOutcome) error {
	memory.final = plan
	memory.writes++
	return nil
}

func (memory *outcomeMemory) Retry(_ context.Context, plan AutomaticRetry) error {
	memory.retry = plan
	memory.writes++
	return nil
}

func outcomesFixture() (*Outcomes, *outcomeMemory) {
	memory := &outcomeMemory{counts: map[string]int64{"PENDING": 2}, budget: RetryBudget{Attempt: 1, Maximum: 4, Deadline: 10000}}
	return NewOutcomes(memory, func() time.Time { return time.UnixMilli(100) }), memory
}

func TestOutcomeCancellationPreservesCompletedResults(t *testing.T) {
	service, memory := outcomesFixture()
	memory.counts["NOT_FOUND"] = 1
	if err := service.Cancel(t.Context(), Work{}); err != nil {
		t.Fatal(err)
	}
	if memory.access != CancelledWorker || memory.final.Counts.Cancelled != 2 || memory.final.Counts.NotFound != 1 || memory.final.PendingState != "CANCELLED" || memory.final.JobState != "CANCELLED" {
		t.Fatalf("cancel plan: %+v", memory.final)
	}
	if memory.counts["PENDING"] != 2 || memory.counts["CANCELLED"] != 0 {
		t.Fatal("cancellation mutated storage snapshot")
	}
}

func TestOutcomeFinishingRequiresEveryItemTerminal(t *testing.T) {
	service, memory := outcomesFixture()
	if err := service.Finish(t.Context(), Work{}); !errors.Is(err, ErrOutcomeIncomplete) || memory.writes != 0 {
		t.Fatalf("incomplete finish: %v", err)
	}
	memory.counts = map[string]int64{"IMPORTED_MATCHED": 1, "COMMIT_FAILED": 1, "NOT_FOUND": 1}
	if err := service.Finish(t.Context(), Work{}); err != nil {
		t.Fatal(err)
	}
	if memory.final.State != "PARTIAL_FAILURE" || memory.final.JobState != "SUCCEEDED" || memory.final.Counts.Failed != 1 || memory.final.Counts.Matched != 1 || memory.final.Counts.NotFound != 1 {
		t.Fatalf("terminal summary: %+v", memory.final)
	}
}

func TestOutcomeRetryKeepsBudgetAndNeverResetsTerminalItems(t *testing.T) {
	service, memory := outcomesFixture()
	at, err := service.Fail(t.Context(), Work{Execution: 2}, "INTERNAL_ERROR")
	if err != nil || at != 1100 || memory.retry.Unit.Execution != 2 || memory.final.State != "" {
		t.Fatalf("automatic retry: %+v %d %v", memory.retry, at, err)
	}
	memory.counts["NOT_FOUND"] = 1
	at, err = service.Fail(t.Context(), Work{}, "INTERNAL_ERROR")
	if err != nil || at != 0 || memory.final.State != "FAILED" || memory.final.Counts.Failed != 2 || memory.final.Counts.NotFound != 1 {
		t.Fatalf("terminal item retry fence: %+v %d %v", memory.final, at, err)
	}
}

func TestOutcomeCommitFailureDoesNotScheduleRetry(t *testing.T) {
	service, memory := outcomesFixture()
	memory.lateErr = context.Canceled
	at, err := service.Fail(t.Context(), Work{}, "INTERNAL_ERROR")
	if !errors.Is(err, context.Canceled) || at != 0 {
		t.Fatalf("uncommitted retry escaped: %d %v", at, err)
	}
	memory.readErr = context.DeadlineExceeded
	if err := service.Finish(t.Context(), Work{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("storage cause lost: %v", err)
	}
}
