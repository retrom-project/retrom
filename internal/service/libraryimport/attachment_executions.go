package libraryimport

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/service/cleanupjobs"
)

type AttachmentExecution struct {
	ID, Kind, ScopeID, State, WorkerID         string
	ExecutionNo, Attempt, MaxAttempts, Version int64
	AvailableMS                                int64
	LeaseMS, DeadlineMS                        *int64
	Cancellable                                bool
	Retryable                                  *bool
}

type AttachmentTransition struct {
	State, Code, Event string
	AvailableMS, NowMS int64
	Retryable          *bool
	Reason             string
}

type AttachmentRecoveryRecords interface {
	cleanupjobs.JobWriter
	FinishedInputs(context.Context) ([]AttachmentInputRelease, error)
	Current(context.Context, string) (AttachmentExecution, error)
	Interrupted(context.Context, int64) ([]AttachmentExecution, error)
	Transition(context.Context, AttachmentExecution, AttachmentTransition) error
}

type AttachmentInputRelease struct {
	ID      string
	Version int64
}

func scheduleFinishedAttachmentInputs(ctx context.Context, records AttachmentRecoveryRecords, now int64) error {
	inputs, err := records.FinishedInputs(ctx)
	if err != nil {
		return fmt.Errorf("read finished attachment inputs: %w", err)
	}
	scheduler := cleanupjobs.NewScheduler(nil)
	for _, input := range inputs {
		if _, err := scheduler.Queue(ctx, records, cleanupjobs.ScheduleRequest{
			Scope:        cleanupjobs.Scope{Type: cleanupjobs.ScopeUploadConsumption, ID: input.ID},
			ScopeVersion: input.Version, Reason: cleanupjobs.ReasonUploadConsumed, NowMS: now,
		}); err != nil {
			return fmt.Errorf("release attachment input: %w", err)
		}
	}
	return nil
}

func (service *AttachmentExecutions) CancelJob(
	ctx context.Context, request ImportJobCancellation,
) (ImportCancellationResult, error) {
	request, err := normalizeImportCancellation(request)
	if err != nil {
		return ImportCancellationResult{}, err
	}
	var result ImportCancellationResult
	err = service.repository.WithRecovery(ctx, func(records AttachmentRecoveryRecords) error {
		before, err := records.Current(ctx, request.JobID)
		if err != nil {
			return fmt.Errorf("read attachment cancellation: %w", err)
		}
		if before.ScopeID != request.ImportID {
			return ErrVersionConflict
		}
		if before.State == "CANCEL_REQUESTED" || before.State == "CANCELLED" {
			result = ImportCancellationResult{JobID: before.ID, State: before.State, ExecutionNo: before.ExecutionNo, Version: before.Version, Pending: before.State == "CANCEL_REQUESTED"}
			return nil
		}
		if !attachmentCancellable(before) {
			return ErrVersionConflict
		}
		state := "CANCELLED"
		if before.State == "RUNNING" {
			state = "CANCEL_REQUESTED"
		}
		now := service.now().UnixMilli()
		if err := records.Transition(ctx, before, AttachmentTransition{
			State: state, Event: state, NowMS: now, AvailableMS: now, Reason: request.Reason,
		}); err != nil {
			return fmt.Errorf("persist attachment cancellation: %w", err)
		}
		result = ImportCancellationResult{
			JobID: before.ID, State: state,
			Version: before.Version + 1, ExecutionNo: before.ExecutionNo, Pending: state == "CANCEL_REQUESTED",
		}
		return scheduleFinishedAttachmentInputs(ctx, records, now)
	})
	if err != nil {
		return ImportCancellationResult{}, fmt.Errorf("cancel attachment: %w", err)
	}
	return result, nil
}

func attachmentCancellable(before AttachmentExecution) bool {
	return before.Cancellable && (before.State == "QUEUED" || before.State == "RUNNING" ||
		before.State == "FAILED" && before.Retryable != nil && *before.Retryable)
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
		return scheduleFinishedAttachmentInputs(ctx, records, now)
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
