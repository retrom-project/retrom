package uploads

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"retrom/internal/cleanup"
)

func matchesRun(current SessionState, run Run) bool {
	return current.ID == run.UploadID && current.State == "FINALIZING" && current.FinalizeJobID != nil &&
		*current.FinalizeJobID == run.JobID && current.FinalizationNo == run.FinalizationNo
}

func (service *Service) claimFinalization(ctx context.Context, run Run) (bool, error) {
	var claimed, cancelled bool
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, err := scope.Sessions.Current(ctx, run.UploadID)
		if err != nil {
			return fmt.Errorf("read finalize owner: %w", err)
		}
		if !matchesRun(current, run) {
			return nil
		}
		job, err := scope.Jobs.Get(ctx, run.JobID)
		if err != nil {
			return fmt.Errorf("read finalize job: %w", err)
		}
		if job.ExecutionNo != run.ExecutionNo {
			return nil
		}
		if job.State == "CANCELLED" {
			cancelled = true
			return finishUploadCancellation(ctx, scope, run, current.Version, service.now().UnixMilli())
		}
		if job.State != "QUEUED" {
			return nil
		}
		claim := JobClaim{Run: run, AtMS: service.now().UnixMilli(), EventJSON: jobEvent(run, 1, "")}
		claimed, err = scope.Jobs.Claim(ctx, claim)
		if err != nil {
			return fmt.Errorf("claim upload job: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("claim upload finalization: %w", err)
	}
	if cancelled {
		cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", run.UploadID))
	}
	return claimed, nil
}

func (service *Service) finalizeWrite(
	ctx context.Context,
	run Run,
	work func(WriteScope, SessionState) error,
) (bool, error) {
	stopped, cancelled := false, false
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, err := scope.Sessions.Current(ctx, run.UploadID)
		if err != nil {
			return fmt.Errorf("read finalization owner: %w", err)
		}
		if !matchesRun(current, run) {
			stopped = true
			return nil
		}
		job, err := scope.Jobs.Get(ctx, run.JobID)
		if err != nil {
			return fmt.Errorf("read finalization execution: %w", err)
		}
		if job.ExecutionNo != run.ExecutionNo {
			stopped = true
			return nil
		}
		if job.State == "CANCEL_REQUESTED" {
			now := service.now().UnixMilli()
			if err := finishUploadCancellation(ctx, scope, run, current.Version, now); err != nil {
				return err
			}
			if err := scope.Jobs.Finish(
				ctx,
				JobFinish{
					Run:           run,
					ExpectedState: "CANCEL_REQUESTED",
					State:         "CANCELLED",
					AtMS:          now,
					EventJSON: jobEvent(
						run,
						1,
						"",
					),
				},
			); err != nil {
				return fmt.Errorf("persist upload transition: %w", err)
			}
			stopped, cancelled = true, true
			return nil
		}
		if job.State != "RUNNING" {
			stopped = true
			return nil
		}
		return work(scope, current)
	})
	if err != nil {
		return false, fmt.Errorf("write upload finalization: %w", err)
	}
	if cancelled {
		cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", run.UploadID))
	}
	return stopped, nil
}

func (service *Service) fail(ctx context.Context, run Run, cause error) error {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	code := "UPLOAD_FINALIZE_IO"
	if errors.Is(cause, errPartMissing) {
		code = "UPLOAD_PART_MISSING"
	}
	if errors.Is(cause, errPartCorrupt) {
		code = "UPLOAD_PART_CORRUPT"
	}
	_, err := service.finalizeWrite(ctx, run, func(scope WriteScope, current SessionState) error {
		now := service.now().UnixMilli()
		if err := scope.Sessions.Finish(
			ctx,
			SessionFinish{
				Run:             run,
				State:           "FAILED",
				ExpectedVersion: current.Version,
				AtMS:            now,
				ErrorCode:       &code,
			},
		); err != nil {
			return fmt.Errorf("persist upload transition: %w", err)
		}
		if err := scope.Files.FailPending(ctx, PendingFailure{UploadID: run.UploadID, Code: code, AtMS: now}); err != nil {
			return fmt.Errorf("persist upload transition: %w", err)
		}
		return scope.Jobs.Finish(
			ctx,
			JobFinish{
				Run:           run,
				ExpectedState: "RUNNING",
				State:         "FAILED",
				ErrorCode:     &code,
				AtMS:          now,
				EventJSON: jobEvent(
					run,
					1,
					code,
				),
			},
		)
	})
	return err
}
