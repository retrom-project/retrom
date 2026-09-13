package uploads

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func finalizationError(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, cause)
}

func currentFinalization(ctx context.Context, scope WriteScope, run Run) (SessionState, Job, bool, error) {
	current, err := scope.Sessions.Current(ctx, run.UploadID)
	if err != nil {
		return current, Job{}, false, finalizationError("read finalize owner", err)
	}
	if !matchesRun(current, run) {
		return current, Job{}, false, nil
	}
	job, err := scope.Jobs.Get(ctx, run.JobID)
	if err != nil {
		return current, job, false, finalizationError("read finalize execution", err)
	}
	return current, job, executionOwned(job, run), nil
}

func (service *Service) finalizeWrite(
	ctx context.Context, run Run, work func(WriteScope, SessionState) error,
) (bool, error) {
	stopped := false
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, job, owned, err := currentFinalization(ctx, scope, run)
		if err != nil {
			return err
		}
		if !owned {
			stopped = true
			return nil
		}
		if err := executionActive(job, run, service.now().UnixMilli()); err != nil {
			return err
		}
		return work(scope, current)
	})
	return stopped, finalizationError("write upload finalization", err)
}

func (service *Service) fail(parent context.Context, run Run, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	cancelled := false
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, job, owned, err := currentFinalization(ctx, scope, run)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		if !owned || job.State != "RUNNING" && job.State != "CANCEL_REQUESTED" || job.Lease <= now && job.Deadline > now {
			return nil
		}
		cancelled = job.State == "CANCEL_REQUESTED"
		return persistFinalizationFailure(ctx, scope, current, job, run, cause, now)
	})
	if err != nil {
		return finalizationError("settle upload failure", err)
	}
	if cancelled {
		return finalizationError("remove cancelled upload", service.source.Remove(ctx, run.UploadID, ""))
	}
	return nil
}

func persistFinalizationFailure(
	ctx context.Context, scope WriteScope, current SessionState, job Job, run Run, cause error, now int64,
) error {
	code, retryable := finalizationFailure(cause, run.Deadline, now)
	state := "FAILED"
	if job.State == "CANCEL_REQUESTED" {
		state = "CANCELLED"
		code = "UPLOAD_CANCELLED"
		retryable = false
	}
	var broken *BrokenPart
	if state == "FAILED" && errors.As(cause, &broken) {
		if err := scope.Finalize.Invalidate(ctx, *broken, now); err != nil {
			return finalizationError("invalidate failed part", err)
		}
	}
	finish := SessionFinish{Run: run, State: state, ExpectedVersion: current.Version, AtMS: now, ErrorCode: &code}
	if err := scope.Sessions.Finish(ctx, finish); err != nil {
		return finalizationError("fail upload session", err)
	}
	if err := scope.Files.FailPending(ctx, PendingFailure{UploadID: run.UploadID, Code: code, AtMS: now}); err != nil {
		return finalizationError("fail pending upload files", err)
	}
	return finalizationError("fail finalize job", scope.Jobs.Finish(ctx, JobFinish{
		Run: run, ExpectedState: job.State, State: state, ErrorCode: &code, Retryable: retryable, AtMS: now,
		EventJSON: finalizationEvent(run, code, cause),
	}))
}

func (service *Service) finalizeFiles(ctx context.Context, claim finalizationClaim) error {
	run := claim.Run
	var files []Candidate
	stopped, err := service.finalizeWrite(ctx, run, func(scope WriteScope, _ SessionState) error {
		var err error
		files, err = scope.Finalize.Candidates(ctx, run.UploadID)
		return finalizationError("read unfinished upload files", err)
	})
	if err != nil {
		return err
	}
	if stopped {
		return ErrExecutionLost
	}
	for _, file := range files {
		stopped, err := service.finalizeCandidate(ctx, run, file)
		if err != nil {
			return err
		}
		if stopped {
			return ErrExecutionLost
		}
	}
	stopped, err = service.finalizeWrite(ctx, run, func(scope WriteScope, current SessionState) error {
		return finishFinalization(ctx, scope, current, run, service.now().UnixMilli())
	})
	if err == nil && stopped {
		return ErrExecutionLost
	}
	return err
}

func finishFinalization(ctx context.Context, scope WriteScope, current SessionState, run Run, now int64) error {
	count, err := scope.Finalize.Count(ctx, run.UploadID)
	if err != nil {
		return finalizationError("count unfinished upload files", err)
	}
	if count != 0 {
		return ErrExecutionLost
	}
	expires := now + (7 * 24 * time.Hour).Milliseconds()
	finish := SessionFinish{
		Run: run, State: "COMPLETE", ExpectedVersion: current.Version, AtMS: now, ExpiresAtMS: &expires,
	}
	if err := scope.Sessions.Finish(ctx, finish); err != nil {
		return finalizationError("complete upload session", err)
	}
	return finalizationError("complete finalize job", scope.Jobs.Finish(ctx, JobFinish{
		Run: run, ExpectedState: "RUNNING", State: "SUCCEEDED", AtMS: now, EventJSON: finalizationEvent(run, "", nil),
	}))
}

func (service *Service) cleanupUpload(parent context.Context, upload, file string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	return finalizationError("clean staged upload", service.source.Remove(ctx, upload, file))
}
