package emulationstationimport

import (
	"context"
	"fmt"
	"math"

	model "retrom/internal/model/emulationstationimport"
)

func (service *ExecutionControl) Fail(
	ctx context.Context,
	unit model.Execution,
	failure model.ExecutionFailure,
) (string, error) {
	for {
		state, more, err := service.failAttempt(ctx, unit, failure)
		if err != nil {
			return "", fmt.Errorf("fail EmulationStation execution: %w", err)
		}
		if !more {
			return state, nil
		}
	}
}

func (service *ExecutionControl) failAttempt(
	ctx context.Context, unit model.Execution, failure model.ExecutionFailure,
) (string, bool, error) {
	before, found, err := service.repository.CurrentExecution(ctx, unit.JobID)
	if err != nil {
		return "", false, fmt.Errorf("read EmulationStation failure: %w", err)
	}
	if !found || before.Execution != unit {
		return "", false, model.ErrVersionConflict
	}
	now := service.now().UnixMilli()
	ownership := ExecutionState(before, unit, now)
	if ownership == model.LeaseDeadline {
		return "", false, model.ErrExpired
	}
	if ownership != model.LeaseActive {
		return "", false, model.ErrVersionConflict
	}
	terminal, err := service.repository.TerminalCount(ctx, unit.ImportID)
	if err != nil {
		return "", false, fmt.Errorf("read EmulationStation retry progress: %w", err)
	}
	change, err := planExecutionFailure(before, failure, terminal, now)
	if err != nil {
		return "", false, err
	}
	if change.JobState != "QUEUED" {
		result, err := service.repository.CommitExecutionReviewBatch(
			ctx, unit, func() int64 { return service.now().UnixMilli() }, before.ReleaseYearMax,
		)
		if err != nil {
			return "", false, err
		}
		if result.More {
			return "", true, nil
		}
		change.Before = result.Before
		change.NowMS = service.now().UnixMilli()
	}
	if err := service.repository.CommitExecutionFinish(ctx, change); err != nil {
		return "", false, fmt.Errorf("persist EmulationStation execution failure: %w", err)
	}
	return change.JobState, false, nil
}

func planExecutionFailure(
	before model.LeaseSnapshot,
	failure model.ExecutionFailure,
	terminal, now int64,
) (model.ExecutionFinish, error) {
	if failure.Code == "" || terminal < 0 || before.MaxAttempts <= 0 {
		return model.ExecutionFinish{}, model.ErrInvalid
	}
	change := model.ExecutionFinish{
		Before: before, NowMS: now, JobState: "FAILED", ImportState: "FAILED",
		ItemState: "COMMIT_FAILED", Code: failure.Code, Retryable: failure.Retryable,
	}
	if !failure.Retryable {
		planExecutionProjection(&change)
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
	planExecutionProjection(&change)
	return change, nil
}

func planExecutionProjection(change *model.ExecutionFinish) {
	scan := change.Before.Kind == "SERVER_EMULATIONSTATION_SCAN"
	change.ClearScan = scan && change.JobState != "FAILED"
	change.TerminalItems = !scan && change.JobState != "QUEUED"
	change.SchedulePayload = change.TerminalItems
	change.RetryFailedItems = !scan && change.JobState == "FAILED"
}
