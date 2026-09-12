package emulationstationimport

import (
	"context"
	"fmt"
	"math"
)

func (service *ExecutionControl) Fail(ctx context.Context, unit Execution, failure ExecutionFailure) (string, error) {
	for {
		state, more := "", false
		err := service.repository.WithExecution(ctx, func(scope ExecutionScope) error {
			var err error
			state, more, err = service.failInScope(ctx, scope, unit, failure)
			return err
		})
		if err != nil {
			return "", fmt.Errorf("fail EmulationStation execution: %w", err)
		}
		if !more {
			return state, nil
		}
	}
}

func (service *ExecutionControl) failInScope(
	ctx context.Context, scope ExecutionScope, unit Execution, failure ExecutionFailure,
) (string, bool, error) {
	before, err := currentExecution(ctx, scope.Read, unit)
	if err != nil {
		return "", false, err
	}
	now := service.now().UnixMilli()
	ownership := ExecutionState(before, unit, now)
	if ownership == LeaseDeadline {
		return "", false, ErrExpired
	}
	if ownership != LeaseActive {
		return "", false, ErrVersionConflict
	}
	terminal, err := scope.Read.TerminalCount(ctx, unit.ImportID)
	if err != nil {
		return "", false, fmt.Errorf("read EmulationStation retry progress: %w", err)
	}
	change, err := planExecutionFailure(before, failure, terminal, now)
	if err != nil {
		return "", false, err
	}
	if change.JobState != "QUEUED" {
		var more bool
		before, more, err = service.completeReviews(ctx, scope, before)
		if err != nil || more {
			return "", more, err
		}
		change.Before, change.NowMS = before, service.now().UnixMilli()
	}
	if err := scope.Write.Finish(ctx, change); err != nil {
		return "", false, fmt.Errorf("persist EmulationStation execution failure: %w", err)
	}
	return change.JobState, false, nil
}

func planExecutionFailure(
	before LeaseSnapshot,
	failure ExecutionFailure,
	terminal, now int64,
) (ExecutionFinish, error) {
	if failure.Code == "" || terminal < 0 || before.MaxAttempts <= 0 {
		return ExecutionFinish{}, ErrInvalid
	}
	change := ExecutionFinish{
		Before: before, NowMS: now, JobState: "FAILED", ImportState: "FAILED",
		ItemState: "COMMIT_FAILED", Code: failure.Code, Retryable: failure.Retryable,
	}
	if !failure.Retryable {
		return change, nil
	}
	delay := RecoveryDelayMS(before.Attempt)
	switch {
	case before.Attempt >= before.MaxAttempts:
		change.Code, change.Retryable = "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED", false
	case now > math.MaxInt64-delay || before.DeadlineAtMS <= now+delay:
		change.Code, change.Retryable = "EMULATIONSTATION_EXECUTION_TIMEOUT", false
	case terminal == 0:
		change.JobState, change.ImportState, change.ItemState = "QUEUED", "QUEUED", ""
		change.AvailableAtMS = now + delay
		if before.Kind == "SERVER_EMULATIONSTATION_SCAN" {
			change.ImportState, change.Phase = "SCANNING", "DISCOVERING_GAMELISTS"
		}
	}
	return change, nil
}
