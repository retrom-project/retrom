package payloadrelease

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/payloadrelease"
)

func (worker *Worker) Finish(ctx context.Context, unit model.Work, executionErr error) error {
	before, err := worker.currentExplicit(ctx, unit)
	if err != nil {
		return fmt.Errorf("finish payload execution: %w", err)
	}
	change, err := worker.settlement(before, executionErr, worker.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("finish payload execution: %w", err)
	}
	if err := worker.prepareOwnerFailure(ctx, &change); err != nil {
		return fmt.Errorf("finish payload execution: %w", err)
	}
	if err := worker.repository.CommitWorkChange(ctx, change); err != nil {
		return fmt.Errorf("finish payload execution: %w", err)
	}
	return nil
}

func (worker *Worker) prepareOwnerFailure(ctx context.Context, change *model.WorkChange) error {
	before := change.Before
	if change.After.State != "FAILED" || before.Scope.Type == model.ScopeUploadConsumption ||
		before.Scope.Type == model.ScopeBlob {
		return nil
	}
	owner, err := worker.repository.LoadWorkOwner(ctx, before.Scope)
	if err != nil {
		return fmt.Errorf("read failed release owner: %w", err)
	}
	if owner.Scope != before.Scope || owner.ReleaseJobID != before.ID {
		return model.ErrExecutionLost
	}
	if owner.PayloadState == "RELEASED" {
		return nil
	}
	if owner.PayloadState != "RELEASING" && owner.PayloadState != "FAILED" {
		return model.ErrScopeInvalid
	}
	change.OwnerFailure = &owner
	return nil
}

func (worker *Worker) settlement(before model.Work, cause error, now int64) (model.WorkChange, error) {
	after := before
	after.Version++
	after.WorkerID = ""
	after.Lease = model.WorkTime{}
	after.Heartbeat = model.WorkTime{}
	after.State = "SUCCEEDED"
	change := model.WorkChange{
		Before: before, After: after, NowMS: now, EventType: "SUCCEEDED", EventJSON: `{"schemaVersion":1}`,
	}
	if cause != nil {
		change.ErrorCode = WorkErrorCode(cause)
		change.Retryable = !errors.Is(cause, model.ErrInputInvalid)
		change.After.State = "FAILED"
		change.EventType = "FAILED"
		if change.Retryable && !errors.Is(cause, model.ErrExecutionTimeout) &&
			executionBudgetFailure(before, now) == nil {
			change.After.State = "QUEUED"
			change.EventType = "RETRY_SCHEDULED"
			change.After.AvailableMS = now + RetryDelay(before.Attempt).Milliseconds()
			if errors.Is(cause, model.ErrExecutionLost) || errors.Is(cause, context.Canceled) ||
				errors.Is(cause, model.ErrWorkerClosed) {
				change.After.AvailableMS = now
			}
		}
		change.EventJSON = fmt.Sprintf(`{"schemaVersion":1,"errorCode":%q,"executionNo":%d,"attempt":%d}`,
			change.ErrorCode, before.ExecutionNo, before.Attempt)
	}
	if before.Kind == "PAYLOAD_RELEASE" && change.After.State != "QUEUED" {
		id, err := worker.newID()
		if err != nil {
			return model.WorkChange{}, fmt.Errorf("create payload settlement audit: %w", err)
		}
		if id == "" {
			return model.WorkChange{}, model.ErrScheduleIDInvalid
		}
		state := "RELEASED"
		change.AuditAction = "PAYLOAD_RELEASE_COMPLETED"
		if cause != nil {
			state = "FAILED"
			change.AuditAction = "PAYLOAD_RELEASE_FAILED"
		}
		change.AuditID = id
		change.AuditJSON = fmt.Sprintf(
			`{"schemaVersion":1,"jobId":%q,"scopeType":%q,"scopeId":%q,"state":%q,"errorCode":%q}`,
			before.ID,
			before.Scope.Type,
			before.Scope.ID,
			state,
			change.ErrorCode,
		)
	}
	return change, nil
}

func RetryDelay(attempt int64) time.Duration {
	switch attempt {
	case 1:
		return time.Second
	case 2:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

func WorkErrorCode(err error) string {
	switch {
	case errors.Is(err, model.ErrInputInvalid):
		return model.ErrInputInvalid.Error()
	case errors.Is(err, model.ErrExecutionTimeout):
		return model.ErrExecutionTimeout.Error()
	case errors.Is(err, model.ErrAttemptsExhausted):
		return model.ErrAttemptsExhausted.Error()
	default:
		var coded interface{ Code() string }
		if errors.As(err, &coded) {
			return coded.Code()
		}
		return "PAYLOAD_RELEASE_DATABASE_FAILED"
	}
}
