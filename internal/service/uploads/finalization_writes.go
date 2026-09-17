package uploads

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/uploads"
)

func finalizationError(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, cause)
}

func (service *Service) fail(parent context.Context, run model.Run, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	cancelled, err := service.repository.CommitFinalizationFailure(ctx, model.FinalizationFailureCommand{
		Run: run, Cause: cause, NowMS: service.now().UnixMilli(),
	})
	if err != nil {
		return finalizationError("settle upload failure", err)
	}
	if cancelled {
		return finalizationError("remove cancelled upload", service.source.Remove(ctx, run.UploadID, ""))
	}
	return nil
}

func (service *Service) finalizeFiles(ctx context.Context, claim finalizationClaim) error {
	run := claim.Run
	now := service.now().UnixMilli()
	files, stopped, err := service.repository.CommitReadCandidates(ctx, model.FinalizationOwnershipCommand{
		Run: run, NowMS: now,
	})
	if err != nil {
		return finalizationError("write upload finalization", err)
	}
	if stopped {
		return model.ErrExecutionLost
	}
	for _, file := range files {
		stopped, err := service.finalizeCandidate(ctx, run, file)
		if err != nil {
			return err
		}
		if stopped {
			return model.ErrExecutionLost
		}
	}
	stopped, err = service.repository.CommitFinishFinalization(ctx, model.FinishFinalizationCommand{
		Run: run, NowMS: service.now().UnixMilli(),
	})
	if err != nil {
		return finalizationError("write upload finalization", err)
	}
	if stopped {
		return model.ErrExecutionLost
	}
	return nil
}

func (service *Service) cleanupUpload(parent context.Context, upload, file string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	return finalizationError("clean staged upload", service.source.Remove(ctx, upload, file))
}
