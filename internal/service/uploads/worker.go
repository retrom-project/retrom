package uploads

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type finalizationClaim struct {
	Run                 Run
	Input               FinalizationInput
	Acquired, Cancelled bool
	Cause               error
}

func matchesRun(current SessionState, run Run) bool {
	return current.ID == run.UploadID && current.State == "FINALIZING" && current.FinalizeJobID != nil &&
		*current.FinalizeJobID == run.JobID && current.FinalizationNo == run.FinalizationNo
}

func (service *Service) claim(ctx context.Context, id string) (finalizationClaim, error) {
	worker, err := uuid.NewV7()
	if err != nil {
		return finalizationClaim{}, fmt.Errorf("create upload worker ID: %w", err)
	}
	var claim finalizationClaim
	err = service.repository.WithWrite(ctx, func(scope WriteScope) error {
		job, err := scope.Jobs.Get(ctx, id)
		if err != nil {
			return finalizationError("read upload authority", err)
		}
		if job.Kind != "UPLOAD_FINALIZE" || job.Scope != "UPLOAD_SESSION" {
			return ErrExecutionLost
		}
		current, err := scope.Sessions.Current(ctx, job.ScopeID)
		if err != nil {
			return finalizationError("read upload authority", err)
		}
		if current.FinalizeJobID == nil || *current.FinalizeJobID != id || current.Consumed {
			return nil
		}
		claim.Run = Run{
			UploadID: current.ID, JobID: id, FinalizationNo: current.FinalizationNo, ExecutionNo: job.ExecutionNo,
			WorkerID: worker.String(), Attempt: job.Attempt + 1, Deadline: job.Deadline,
		}
		return service.claimCurrent(ctx, scope, current, job, &claim)
	})
	if err != nil {
		return finalizationClaim{}, fmt.Errorf("claim upload finalization: %w", err)
	}
	if claim.Cancelled {
		if err := service.cleanupUpload(ctx, claim.Run.UploadID, ""); err != nil {
			return claim, finalizationError("remove cancelled upload", err)
		}
	}
	return claim, nil
}

func (service *Service) claimCurrent(
	ctx context.Context, scope WriteScope, current SessionState, job Job, claim *finalizationClaim,
) error {
	now := service.now().UnixMilli()
	if job.State == "CANCELLED" && matchesRun(current, claim.Run) {
		return claim.reconcile(ctx, scope, current, job, now)
	}
	if job.State == "RUNNING" || job.State == "CANCEL_REQUESTED" {
		if job.Lease > now && job.Deadline > now {
			return nil
		}
		claim.Run.Attempt = job.Attempt
		if job.State == "RUNNING" && job.Deadline > now && job.Attempt < job.MaxAttempts {
			return finalizationError("requeue expired upload", scope.Leases.Requeue(ctx, job, now, min(now+1000, job.Deadline)))
		}
	} else if job.State != "QUEUED" {
		return nil
	}
	switch current.State {
	case "FINALIZING", "FAILED":
	default:
		return nil
	}
	if err := claim.prepare(ctx, scope, current, job, now); err != nil {
		return err
	}
	if claim.Cause == nil && job.Available > now {
		return nil
	}
	return claim.acquire(ctx, scope, current, job, now)
}

func (claim *finalizationClaim) prepare(
	ctx context.Context, scope WriteScope, current SessionState, job Job, now int64,
) error {
	claim.Input, claim.Cause = decodeFinalization(job)
	if claim.Cause == nil && claim.Input.Inputs.FinalizationNo != current.FinalizationNo {
		claim.Cause = ErrInputInvalid
	}
	if claim.Cause == nil {
		files, err := scope.Finalize.Manifest(ctx, current.ID)
		if err != nil {
			return finalizationError("read frozen upload files", err)
		}
		claim.Cause = validateFinalizationFiles(claim.Input, files)
	}
	switch {
	case job.State == "CANCEL_REQUESTED":
		claim.Cause = context.Canceled
	case job.Deadline > 0 && job.Deadline <= now:
		claim.Cause = context.DeadlineExceeded
	case job.Attempt >= job.MaxAttempts:
		claim.Cause = ErrAttemptsExhausted
	}
	if claim.Cause != nil {
		claim.Run.Attempt = job.Attempt
	}
	if claim.Run.Deadline == 0 {
		claim.Run.Deadline = now + finalizationTimeout.Milliseconds()
	}
	return nil
}

func (claim *finalizationClaim) acquire(
	ctx context.Context, scope WriteScope, current SessionState, job Job, now int64,
) error {
	if current.State == "FAILED" {
		progress := SessionProgress{ID: current.ID, State: "FINALIZING", ExpectedVersion: current.Version, AtMS: now}
		if err := scope.Sessions.Advance(ctx, progress); err != nil {
			return finalizationError("resume upload session", err)
		}
		if err := scope.Files.MarkFinalizing(ctx, current.ID, now); err != nil {
			return finalizationError("resume upload files", err)
		}
	}
	input := JobClaim{Run: claim.Run, Version: job.Version, AtMS: now, EventJSON: finalizationEvent(claim.Run, "", nil)}
	claimed, err := scope.Jobs.Claim(ctx, input)
	claim.Acquired = claimed
	return finalizationError("claim finalize job", err)
}

func (service *Service) Run(parent context.Context, id string) error {
	claim, err := service.claim(parent, id)
	if err != nil || !claim.Acquired {
		return err
	}
	if claim.Cause != nil {
		return errors.Join(claim.Cause, service.fail(parent, claim.Run, claim.Cause))
	}
	remaining := time.Duration(claim.Run.Deadline-service.now().UnixMilli()) * time.Millisecond
	ctx, timeout := context.WithTimeout(parent, remaining)
	defer timeout()
	ctx, cancel := context.WithCancelCause(ctx)
	stopped := make(chan struct{})
	go service.monitor(ctx, cancel, claim.Run, stopped)
	defer func() { cancel(nil); <-stopped }()
	err = service.finalizeFiles(ctx, claim)
	cause := errors.Join(err, context.Cause(ctx))
	if cause != nil {
		return errors.Join(cause, service.fail(ctx, claim.Run, cause))
	}
	return nil
}

func (service *Service) monitor(ctx context.Context, cancel context.CancelCauseFunc, run Run, stopped chan<- struct{}) {
	defer close(stopped)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := service.now().UnixMilli()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := service.now().UnixMilli()
		err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
			job, err := scope.Jobs.Get(ctx, run.JobID)
			if err != nil {
				return finalizationError("observe upload job", err)
			}
			if job.State == "CANCEL_REQUESTED" && executionOwned(job, run) {
				return context.Canceled
			}
			if err := executionActive(job, run, now); err != nil {
				return finalizationError("observe upload job", err)
			}
			if now-last >= 15000 {
				return scope.Leases.Refresh(ctx, run, now)
			}
			return nil
		})
		if err != nil {
			cancel(fmt.Errorf("observe upload authority: %w", err))
			return
		}
		if now-last >= 15000 {
			last = now
		}
	}
}

func (claim *finalizationClaim) reconcile(
	ctx context.Context, scope WriteScope, current SessionState, job Job, now int64,
) error {
	input, err := decodeFinalization(job)
	if err != nil {
		return err
	}
	if input.Inputs.FinalizationNo != current.FinalizationNo {
		return ErrExecutionLost
	}
	claim.Cancelled = true
	return finishUploadCancellation(ctx, scope, claim.Run, current.Version, now)
}
