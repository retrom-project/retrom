package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

type (
	WorkflowSnapshot struct {
		Summary               Summary
		JobState              string
		JobVersion, Execution int64
		OtherActive           bool
		RetryableItems        int64
	}
	WorkflowScope struct {
		Read  WorkflowReader
		Write WorkflowWriter
	}
	WorkflowReader interface {
		Current(context.Context, string) (WorkflowSnapshot, error)
		CurrentJob(context.Context, string) (WorkflowSnapshot, error)
	}
	WorkflowWriter interface {
		Cancel(context.Context, CancellationPlan) error
		Retry(context.Context, RetryPlan) error
	}
	WorkflowRepository interface {
		WithControl(context.Context, func(WorkflowScope) error) error
	}
	CancellationPlan struct {
		Before                   WorkflowSnapshot
		State                    string
		Pending                  bool
		CompletedAtMS            *int64
		Reason, ActorID, AuditID string
		NowMS                    int64
	}
	RetryPlan struct {
		Before                        WorkflowSnapshot
		Execution                     int64
		ExecutionID, AuditID, ActorID string
		NowMS                         int64
	}
	WorkflowControl struct {
		repository WorkflowRepository
		now        func() time.Time
	}
)

func NewWorkflowControl(repository WorkflowRepository, now func() time.Time) *WorkflowControl {
	return &WorkflowControl{repository: repository, now: now}
}

func validWorkflowVersion(before WorkflowSnapshot, version int64) bool {
	return version > 0 && version < math.MaxInt64 && before.Summary.Version == version &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 && before.Execution > 0
}

func (service *WorkflowControl) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	var result Summary
	err := service.repository.WithControl(ctx, func(scope WorkflowScope) error {
		before, err := scope.Read.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read Pegasus retry: %w", err)
		}
		if !canRetry(before, version) {
			return ErrNotRetryable
		}
		plan, err := service.retryPlan(before, actorID)
		if err != nil {
			return err
		}
		if err := scope.Write.Retry(ctx, plan); err != nil {
			return fmt.Errorf("save Pegasus retry: %w", err)
		}
		after, err := scope.Read.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read retried Pegasus import: %w", err)
		}
		result = after.Summary
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("finish Pegasus retry: %w", err)
	}
	return result, nil
}

func canRetry(before WorkflowSnapshot, version int64) bool {
	if !validWorkflowVersion(before, version) || before.Execution == math.MaxInt64 || before.Summary.ImportJobID == nil {
		return false
	}
	if !before.Summary.Retryable || before.OtherActive || before.RetryableItems == 0 {
		return false
	}
	return (before.Summary.State == "FAILED" || before.Summary.State == "PARTIAL_FAILURE") &&
		(before.JobState == "FAILED" || before.JobState == "SUCCEEDED")
}

func (service *WorkflowControl) retryPlan(before WorkflowSnapshot, actorID string) (RetryPlan, error) {
	executionID, err := uuid.NewV7()
	if err != nil {
		return RetryPlan{}, fmt.Errorf("generate Pegasus retry execution: %w", err)
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return RetryPlan{}, fmt.Errorf("generate Pegasus retry audit: %w", err)
	}
	return RetryPlan{
		Before:      before,
		Execution:   before.Execution + 1,
		ExecutionID: executionID.String(),
		AuditID:     auditID.String(),
		ActorID:     actorID,
		NowMS:       service.now().UnixMilli(),
	}, nil
}
