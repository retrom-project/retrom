package payloadrelease

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (worker *Worker) Finish(ctx context.Context, unit Work, executionErr error) error {
	err := worker.repository.WithWorker(ctx, func(scope WorkerScope) error {
		before, err := worker.current(ctx, scope, unit)
		if err != nil {
			return err
		}
		return worker.settle(ctx, scope, before, executionErr, worker.now().UnixMilli())
	})
	if err != nil {
		return fmt.Errorf("finish payload execution: %w", err)
	}
	return nil
}

func (worker *Worker) settle(ctx context.Context, scope WorkerScope, before Work, cause error, now int64) error {
	change, err := worker.settlement(before, cause, now)
	if err != nil {
		return err
	}
	if err := prepareOwnerFailure(ctx, scope, &change); err != nil {
		return err
	}
	if err := scope.Write.Change(ctx, change); err != nil {
		return fmt.Errorf("settle payload work: %w", err)
	}
	return nil
}

func (worker *Worker) settlement(before Work, cause error, now int64) (WorkChange, error) {
	after := before
	after.Version++
	after.WorkerID = ""
	after.Lease = WorkTime{}
	after.Heartbeat = WorkTime{}
	after.State = "SUCCEEDED"
	change := WorkChange{
		Before: before, After: after, NowMS: now, EventType: "SUCCEEDED", EventJSON: `{"schemaVersion":1}`,
	}
	if cause != nil {
		change.ErrorCode = WorkErrorCode(cause)
		change.Retryable = !errors.Is(cause, ErrInputInvalid)
		change.After.State = "FAILED"
		change.EventType = "FAILED"
		if change.Retryable && !errors.Is(cause, ErrExecutionTimeout) && executionBudgetFailure(before, now) == nil {
			change.After.State = "QUEUED"
			change.EventType = "RETRY_SCHEDULED"
			change.After.AvailableMS = now + RetryDelay(before.Attempt).Milliseconds()
			if errors.Is(cause, ErrExecutionLost) || errors.Is(cause, context.Canceled) || errors.Is(cause, ErrWorkerClosed) {
				change.After.AvailableMS = now
			}
		}
		change.EventJSON = fmt.Sprintf(`{"schemaVersion":1,"errorCode":%q,"executionNo":%d,"attempt":%d}`,
			change.ErrorCode, before.ExecutionNo, before.Attempt)
	}
	if before.Kind == "PAYLOAD_RELEASE" && change.After.State != "QUEUED" {
		id, err := worker.newID()
		if err != nil {
			return WorkChange{}, fmt.Errorf("create payload settlement audit: %w", err)
		}
		if id == "" {
			return WorkChange{}, ErrScheduleIDInvalid
		}
		state := "RELEASED"
		change.AuditAction = "PAYLOAD_RELEASE_COMPLETED"
		if cause != nil {
			state = "FAILED"
			change.AuditAction = "PAYLOAD_RELEASE_FAILED"
		}
		change.AuditID = id
		change.AuditJSON = fmt.Sprintf(`{"schemaVersion":1,"jobId":%q,"scopeType":%q,"scopeId":%q,"state":%q,"errorCode":%q}`,
			before.ID, before.Scope.Type, before.Scope.ID, state, change.ErrorCode)
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
	case errors.Is(err, ErrInputInvalid):
		return ErrInputInvalid.Error()
	case errors.Is(err, ErrExecutionTimeout):
		return ErrExecutionTimeout.Error()
	case errors.Is(err, ErrAttemptsExhausted):
		return ErrAttemptsExhausted.Error()
	default:
		var coded interface{ Code() string }
		if errors.As(err, &coded) {
			return coded.Code()
		}
		return "PAYLOAD_RELEASE_DATABASE_FAILED"
	}
}

func prepareOwnerFailure(ctx context.Context, scope WorkerScope, change *WorkChange) error {
	before := change.Before
	if change.After.State != "FAILED" || before.Scope.Type == ScopeUploadConsumption || before.Scope.Type == ScopeBlob {
		return nil
	}
	owner, err := scope.Owners.Owner(ctx, before.Scope)
	if err != nil {
		return fmt.Errorf("read failed release owner: %w", err)
	}
	if owner.Scope != before.Scope || owner.ReleaseJobID != before.ID {
		return ErrExecutionLost
	}
	if owner.PayloadState == "RELEASED" {
		return nil
	}
	if owner.PayloadState != "RELEASING" && owner.PayloadState != "FAILED" {
		return ErrScopeInvalid
	}
	change.OwnerFailure = &owner
	return nil
}
