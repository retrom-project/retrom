package serverimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/serverimport"
)

type outcomeMemory struct {
	finishCmd  model.FinishCommand
	cancelCmd  model.CancelOutcomeCommand
	failCmd    model.FailCommand
	itemPlan   model.ItemOutcome
	failResult model.FailResult
	writes     int
	finishErr  error
	cancelErr  error
	failErr    error
	itemErr    error
}

func (memory *outcomeMemory) CommitItemOutcome(
	_ context.Context, plan model.ItemOutcome,
) error {
	memory.itemPlan = plan
	memory.writes++
	return memory.itemErr
}

func (memory *outcomeMemory) CommitFinish(
	_ context.Context, cmd model.FinishCommand,
) error {
	memory.finishCmd = cmd
	if memory.finishErr != nil {
		return memory.finishErr
	}
	memory.writes++
	return nil
}

func (memory *outcomeMemory) CommitCancelOutcome(
	_ context.Context, cmd model.CancelOutcomeCommand,
) error {
	memory.cancelCmd = cmd
	if memory.cancelErr != nil {
		return memory.cancelErr
	}
	memory.writes++
	return nil
}

func (memory *outcomeMemory) CommitFail(
	_ context.Context, cmd model.FailCommand,
) (model.FailResult, error) {
	memory.failCmd = cmd
	if memory.failErr != nil {
		return model.FailResult{}, memory.failErr
	}
	memory.writes++
	return memory.failResult, nil
}

func (*outcomeMemory) Recovery(
	context.Context, int64,
) (model.RecoveryWork, bool, error) {
	return model.RecoveryWork{}, false, nil
}

func outcomesFixture() (*Outcomes, *outcomeMemory) {
	memory := &outcomeMemory{
		failResult: model.FailResult{RetryAt: 0},
	}
	return NewOutcomes(
		memory, func() time.Time { return time.UnixMilli(100) },
	), memory
}

func TestOutcomeCancelDelegatesToRepo(t *testing.T) {
	service, memory := outcomesFixture()
	if err := service.Cancel(t.Context(), model.Work{}); err != nil {
		t.Fatal(err)
	}
	if memory.writes != 1 {
		t.Fatalf("cancel writes: %d", memory.writes)
	}
}

func TestOutcomeFinishDelegatesToRepo(t *testing.T) {
	service, memory := outcomesFixture()
	if err := service.Finish(t.Context(), model.Work{}); err != nil {
		t.Fatal(err)
	}
	if memory.writes != 1 {
		t.Fatalf("finish writes: %d", memory.writes)
	}
}

func TestOutcomeFailDelegatesToRepo(t *testing.T) {
	service, memory := outcomesFixture()
	memory.failResult = model.FailResult{RetryAt: 1100}
	at, err := service.Fail(
		t.Context(), model.Work{Execution: 2}, "INTERNAL_ERROR",
	)
	if err != nil || at != 1100 {
		t.Fatalf("automatic retry: %d %v", at, err)
	}
}

func TestOutcomeCommitFailureDoesNotScheduleRetry(t *testing.T) {
	service, memory := outcomesFixture()
	memory.failErr = context.Canceled
	at, err := service.Fail(
		t.Context(), model.Work{}, "INTERNAL_ERROR",
	)
	if !errors.Is(err, context.Canceled) || at != 0 {
		t.Fatalf("uncommitted retry escaped: %d %v", at, err)
	}
	memory.finishErr = context.DeadlineExceeded
	if err := service.Finish(
		t.Context(), model.Work{},
	); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("storage cause lost: %v", err)
	}
}
