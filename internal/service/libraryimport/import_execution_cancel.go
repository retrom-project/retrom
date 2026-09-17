package libraryimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"strings"
	"unicode/utf8"
)

func (service *ImportExecutions) CancelJob(
	ctx context.Context,
	request model.ImportJobCancellation,
) (model.ImportCancellationResult, error) {
	request, err := normalizeImportCancellation(request)
	if err != nil {
		return model.ImportCancellationResult{}, err
	}
	var result model.ImportCancellationResult
	err = service.repository.WithExecution(
		ctx,
		func(scope model.ImportExecutionScope) error {
			before, found, err := scope.Records.Current(ctx, request.JobID)
			if err != nil {
				return fmt.Errorf("read import cancellation: %w", err)
			}
			if !found || before.Creation.Execution.ImportID != request.ImportID ||
				before.Creation.JobVersion != request.ExpectedVersion ||
				!before.Cancellable ||
				!importCancellable(before) {
				return model.ErrVersionConflict
			}
			now := service.now().UnixMilli()
			change, err := importCancellationProjection(before, request.Reason, now)
			if err != nil {
				return err
			}
			pending := change.Job.State == "CANCEL_REQUESTED"
			if err := scope.Records.Transition(ctx, change); err != nil {
				return fmt.Errorf("cancel import execution: %w", err)
			}
			if !pending {
				if err := scheduleImportTerminal(ctx, scope, request.ImportID, now); err != nil {
					return err
				}
			}
			result = model.ImportCancellationResult{
				JobID:       request.JobID,
				State:       change.Job.State,
				ExecutionNo: before.Creation.Execution.ExecutionNo,
				Version:     before.Creation.JobVersion + 1,
				Pending:     pending,
			}
			return nil
		},
	)
	if err != nil {
		return model.ImportCancellationResult{}, fmt.Errorf("cancel import job: %w", err)
	}
	return result, nil
}

func importCancellable(before model.ImportWorkerSnapshot) bool {
	state := before.Creation.JobState
	return state == "QUEUED" || state == "RUNNING" || state == "FAILED" && before.Retryable != nil && *before.Retryable
}

func (service *ImportExecutions) SyncCancellation(ctx context.Context, id string) error {
	err := service.repository.WithExecution(ctx, func(scope model.ImportExecutionScope) error {
		before, found, err := scope.Records.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read cancelled import: %w", err)
		}
		if !found || before.Creation.JobState != "CANCELLED" {
			return nil
		}
		return service.syncCancelled(ctx, scope, before, service.now().UnixMilli())
	})
	if err != nil {
		return fmt.Errorf("synchronize import cancellation: %w", err)
	}
	return nil
}

func (service *ImportExecutions) syncCancelled(
	ctx context.Context,
	scope model.ImportExecutionScope,
	before model.ImportWorkerSnapshot,
	now int64,
) error {
	if before.Creation.ImportState != "CANCELLED" {
		change, err := importCancelledTransition(before, "任务已取消", now)
		if err != nil {
			return err
		}
		change.ParentOnly = true
		change.Event = nil
		if err := scope.Records.Transition(ctx, change); err != nil {
			return fmt.Errorf("project cancelled import: %w", err)
		}
	}
	return scheduleImportTerminal(ctx, scope, before.Creation.Execution.ImportID, now)
}

func importCancellationProjection(
	before model.ImportWorkerSnapshot,
	reason string,
	now int64,
) (model.ImportWorkerTransition, error) {
	change, err := importCancelledTransition(before, reason, now)
	if err != nil {
		return model.ImportWorkerTransition{}, err
	}
	if before.Creation.JobState == "RUNNING" {
		change.Job.Execution = before.Creation.Execution
		change.Job.LeaseUntilMS = before.LeaseUntilMS
		change.Job.HeartbeatAtMS = before.HeartbeatAtMS
		change.Job.State = "CANCEL_REQUESTED"
		change.Job.FinishedAtMS = nil
		change.Parent.State = "CANCEL_REQUESTED"
		change.Parent.CompletedAtMS = nil
	}
	event, err := importWorkerEvent(
		before.Creation.Execution,
		change.Job.State,
		now,
		map[string]any{"state": change.Job.State, "reason": reason},
	)
	if err != nil {
		return model.ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}

func normalizeImportCancellation(request model.ImportJobCancellation) (model.ImportJobCancellation, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.JobID == "" || request.ImportID == "" || request.ExpectedVersion < 1 || request.Reason == "" ||
		utf8.RuneCountInString(request.Reason) > 500 {
		return model.ImportJobCancellation{}, model.ErrInvalid
	}
	return request, nil
}
