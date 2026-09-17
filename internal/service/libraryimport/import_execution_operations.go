package libraryimport

import (
	"context"
	"errors"
	"fmt"
	model "retrom/internal/model/libraryimport"
)

func (service *ImportExecutions) owned(ctx context.Context, execution model.QueuedImportExecution, deadline bool,
	operation func(model.ImportExecutionScope, model.ImportWorkerSnapshot, int64) error,
) error {
	err := service.repository.WithExecution(ctx, func(scope model.ImportExecutionScope) error {
		before, found, err := scope.Records.Current(ctx, execution.JobID)
		if err != nil {
			return fmt.Errorf("read import execution: %w", err)
		}
		now := service.now().UnixMilli()
		if !found || !ImportExecutionCurrent(execution, before.Creation, now) {
			return model.ErrVersionConflict
		}
		if err := operation(scope, before, now); err != nil {
			return err
		}
		finished := service.now().UnixMilli()
		if before.Creation.LeaseUntilMS <= finished || deadline && execution.DeadlineMS <= finished {
			return model.ErrVersionConflict
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("change import execution: %w", err)
	}
	return nil
}

func (service *ImportExecutions) Renew(ctx context.Context, execution model.QueuedImportExecution) (bool, error) {
	cancelled := false
	err := service.owned(
		ctx,
		execution,
		true,
		func(scope model.ImportExecutionScope, before model.ImportWorkerSnapshot, now int64) error {
			if before.Creation.JobState == "CANCEL_REQUESTED" {
				cancelled = true
				return nil
			}
			if execution.DeadlineMS <= now {
				return context.DeadlineExceeded
			}
			change := importTransition(before, now)
			change.Parent = nil
			change.RequireLiveLease = true
			change.Job.LeaseUntilMS = importMoment(min(now+model.ImportExecutionLease.Milliseconds(), execution.DeadlineMS))
			change.Job.HeartbeatAtMS = importMoment(now)
			if err := scope.Records.Transition(ctx, change); err != nil {
				return fmt.Errorf("renew import lease: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return false, err
	}
	return cancelled, nil
}

func (service *ImportExecutions) Progress(
	ctx context.Context,
	execution model.QueuedImportExecution,
	itemCount int,
) error {
	if itemCount < 0 {
		return model.ErrInvalid
	}
	return service.owned(
		ctx,
		execution,
		true,
		func(scope model.ImportExecutionScope, before model.ImportWorkerSnapshot, now int64) error {
			if before.Creation.JobState != "RUNNING" || execution.DeadlineMS <= now {
				return model.ErrVersionConflict
			}
			event, err := importWorkerEvent(execution, "PROGRESS", now, map[string]any{
				"phase": "PERSISTING", "completedUnits": itemCount, "totalUnits": itemCount, "unit": "ITEM",
			})
			if err != nil {
				return err
			}
			change := importTransition(before, now)
			change.Parent = nil
			change.RequireLiveLease = true
			change.Event = &event
			if err := scope.Records.Transition(ctx, change); err != nil {
				return fmt.Errorf("record import progress: %w", err)
			}
			return nil
		},
	)
}

func (service *ImportExecutions) Fail(ctx context.Context, execution model.QueuedImportExecution, cause error) error {
	if cause == nil {
		return model.ErrInvalid
	}
	return service.owned(
		ctx,
		execution,
		false,
		func(scope model.ImportExecutionScope, before model.ImportWorkerSnapshot, now int64) error {
			change, release, err := importFailureProjection(before, cause, now)
			if err != nil {
				return err
			}
			change.RequireLiveLease = true
			if err := scope.Records.Transition(ctx, change); err != nil {
				return fmt.Errorf("settle import failure: %w", err)
			}
			if release {
				return scheduleImportTerminal(ctx, scope, execution.ImportID, now)
			}
			return nil
		},
	)
}

func importFailureProjection(
	before model.ImportWorkerSnapshot,
	cause error,
	now int64,
) (model.ImportWorkerTransition, bool, error) {
	if before.Creation.JobState == "CANCEL_REQUESTED" {
		change, err := importCancelledTransition(before, "任务已取消", now)
		return change, true, err
	}
	code, retryable := ImportFailure(cause)
	execution := before.Creation.Execution
	if execution.DeadlineMS <= now {
		change, err := importFailedTransition(before, "IMPORT_GROUP_EXECUTION_TIMEOUT", true, now)
		return change, false, err
	}
	if errors.Is(cause, ErrImportWorkerClosed) || errors.Is(cause, context.Canceled) {
		change, err := importRetryTransition(before, "IMPORT_GROUP_INTERRUPTED", now, now)
		return change, false, err
	}
	if retryable && execution.Attempt < before.Creation.MaxAttempts && execution.DeadlineMS > now {
		available := now + importRetryDelay(execution.Attempt).Milliseconds()
		if available < execution.DeadlineMS {
			change, err := importRetryTransition(before, code, now, available)
			return change, false, err
		}
	}
	change, err := importFailedTransition(before, code, retryable, now)
	return change, !retryable, err
}
