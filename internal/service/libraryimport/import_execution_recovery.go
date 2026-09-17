package libraryimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/model/importprogress"
)

func (service *ImportExecutions) Queued(ctx context.Context) ([]string, error) {
	ids, err := service.repository.Queued(ctx, service.now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read queued import executions: %w", err)
	}
	return ids, nil
}

func (service *ImportExecutions) Recover(ctx context.Context) error {
	ids, err := service.repository.Recoverable(ctx, service.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("read interrupted imports: %w", err)
	}
	for _, id := range ids {
		if err := service.recoverOne(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (service *ImportExecutions) recoverOne(ctx context.Context, id string) error {
	err := service.repository.WithExecution(ctx, func(scope model.ImportExecutionScope) error {
		before, found, err := scope.Records.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read interrupted import: %w", err)
		}
		if !found {
			return nil
		}
		now := service.now().UnixMilli()
		if before.Creation.JobState == "CANCELLED" {
			return service.syncCancelled(ctx, scope, before, now)
		}
		if !importRecoveryRequired(before, now) {
			return nil
		}
		change, release, err := importRecoveryProjection(before, now)
		if err != nil {
			return err
		}
		if err := scope.Records.Transition(ctx, change); err != nil {
			return fmt.Errorf("recover import execution: %w", err)
		}
		if release {
			return scheduleImportTerminal(ctx, scope, before.Creation.Execution.ImportID, now)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("recover import worker: %w", err)
	}
	return nil
}

func importRecoveryRequired(before model.ImportWorkerSnapshot, now int64) bool {
	execution := before.Creation.Execution
	expiredBudget := before.DeadlineAtMS != nil && *before.DeadlineAtMS <= now
	switch before.Creation.JobState {
	case "QUEUED":
		return expiredBudget || (before.ItemCount > 0 || before.ResolvedFiles > 0) ||
			execution.Attempt >= before.Creation.MaxAttempts
	case "RUNNING", "CANCEL_REQUESTED":
		return expiredBudget || before.Creation.LeaseUntilMS <= now
	default:
		return false
	}
}

func importRecoveryProjection(
	before model.ImportWorkerSnapshot,
	now int64,
) (model.ImportWorkerTransition, bool, error) {
	if before.Creation.JobState == "CANCEL_REQUESTED" {
		change, err := importCancelledTransition(before, "任务已取消", now)
		return change, true, err
	}
	if before.ItemCount > 0 || before.ResolvedFiles > 0 {
		change, err := importProducedProjection(before, now)
		return change, true, err
	}
	execution := before.Creation.Execution
	if before.DeadlineAtMS != nil && *before.DeadlineAtMS <= now {
		change, err := importFailedTransition(before, "IMPORT_GROUP_EXECUTION_TIMEOUT", true, now)
		return change, false, err
	}
	if execution.Attempt >= before.Creation.MaxAttempts {
		change, err := importFailedTransition(before, "IMPORT_GROUP_FAILED", true, now)
		return change, false, err
	}
	change, err := importRetryTransition(before, "IMPORT_GROUP_INTERRUPTED", now, now)
	return change, false, err
}

func importProducedProjection(before model.ImportWorkerSnapshot, now int64) (model.ImportWorkerTransition, error) {
	progress, err := importprogress.Project(importprogress.Snapshot{
		State: before.Creation.ImportState, Counts: before.Counts, Started: true,
		CancelRequestedAtMS: before.ParentCancelRequestedAtMS, CompletedAtMS: before.ParentCompletedAtMS,
	}, now)
	if err != nil {
		return model.ImportWorkerTransition{}, fmt.Errorf("recover existing import results: %w", err)
	}
	change := importTransition(before, now)
	clearImportExecutionOwner(&change)
	change.Job.State = "SUCCEEDED"
	change.Job.ErrorCode = nil
	change.Job.Retryable = nil
	change.Job.FinishedAtMS = importMoment(now)
	change.Parent.State = progress.State
	change.Parent.CompletedAtMS = progress.CompletedAtMS
	change.Parent.ErrorCode = nil
	event, err := importWorkerEvent(
		before.Creation.Execution,
		"SUCCEEDED",
		now,
		map[string]any{"schemaVersion": 1, "itemCount": before.ItemCount, "recovered": true},
	)
	if err != nil {
		return model.ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}
