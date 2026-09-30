package libraryimport

import (
	"context"
	"fmt"
	"time"
)

type AttachmentExecution struct {
	ID, Kind, ScopeID, State, WorkerID         string
	ExecutionNo, Attempt, MaxAttempts, Version int64
	AvailableMS                                int64
	LeaseMS, DeadlineMS                        *int64
}

type AttachmentTransition struct {
	State, Code, Event string
	AvailableMS, NowMS int64
	Retryable          *bool
}

type AttachmentRecoveryRecords interface {
	Interrupted(context.Context, int64) ([]AttachmentExecution, error)
	Transition(context.Context, AttachmentExecution, AttachmentTransition) error
}

type AttachmentExecutionRepository interface {
	WithRecovery(context.Context, func(AttachmentRecoveryRecords) error) error
	Queued(context.Context, int64) ([]AttachmentExecution, error)
}

type AttachmentExecutions struct {
	repository AttachmentExecutionRepository
	now        func() time.Time
}

func NewAttachmentExecutions(repository AttachmentExecutionRepository, now func() time.Time) *AttachmentExecutions {
	return &AttachmentExecutions{repository: repository, now: now}
}

func (service *AttachmentExecutions) Queued(ctx context.Context) ([]AttachmentExecution, error) {
	jobs, err := service.repository.Queued(ctx, service.now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read queued attachments: %w", err)
	}
	return jobs, nil
}

func (service *AttachmentExecutions) Recover(ctx context.Context) error {
	err := service.repository.WithRecovery(ctx, func(records AttachmentRecoveryRecords) error {
		now := service.now().UnixMilli()
		jobs, err := records.Interrupted(ctx, now)
		if err != nil {
			return fmt.Errorf("read interrupted attachments: %w", err)
		}
		for _, job := range jobs {
			if change, needed := attachmentRecovery(job, now); needed {
				if err := records.Transition(ctx, job, change); err != nil {
					return fmt.Errorf("transition interrupted attachment: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("recover attachments: %w", err)
	}
	return nil
}

func attachmentRecovery(job AttachmentExecution, now int64) (AttachmentTransition, bool) {
	change := AttachmentTransition{NowMS: now, AvailableMS: now}
	expired := job.DeadlineMS != nil && *job.DeadlineMS <= now
	leaseExpired := job.LeaseMS == nil || *job.LeaseMS <= now
	switch job.State {
	case "CANCELLED":
		change.State = "CANCELLED"
		return change, true
	case "CANCEL_REQUESTED", "RUNNING":
		if !expired && !leaseExpired {
			return change, false
		}
		if job.State == "CANCEL_REQUESTED" {
			change.State, change.Event = "CANCELLED", "CANCELLED"
			return change, true
		}
	case "QUEUED":
		if !expired && job.Attempt < job.MaxAttempts {
			return change, false
		}
	default:
		return change, false
	}
	if expired || job.Attempt >= job.MaxAttempts {
		retryable := true
		change.State, change.Code = "FAILED", "ATTACHMENT_EXECUTION_EXHAUSTED"
		change.Event, change.Retryable = "FAILED", &retryable
		if expired {
			change.Code = "ATTACHMENT_EXECUTION_TIMEOUT"
		}
	} else {
		change.State, change.Code, change.Event = "QUEUED", "ATTACHMENT_INTERRUPTED", "RETRY_SCHEDULED"
		change.AvailableMS = now + 1000
	}
	return change, true
}
