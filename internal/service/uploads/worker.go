package uploads

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/uploads"

	"github.com/google/uuid"
)

type finalizationClaim = model.ClaimResult

func (service *Service) claim(ctx context.Context, id string) (finalizationClaim, error) {
	worker, err := uuid.NewV7()
	if err != nil {
		return finalizationClaim{}, fmt.Errorf("create upload worker ID: %w", err)
	}
	result, err := service.repository.CommitClaimFinalization(ctx, model.ClaimFinalizationCommand{
		JobID: id, WorkerID: worker.String(), NowMS: service.now().UnixMilli(),
		Deadline: service.now().Add(10 * time.Minute).UnixMilli(),
	})
	if err != nil {
		return finalizationClaim{}, fmt.Errorf("claim upload finalization: %w", err)
	}
	if result.Cancelled {
		if err := service.cleanupUpload(ctx, result.Run.UploadID, ""); err != nil {
			return result, finalizationError("remove cancelled upload", err)
		}
	}
	return result, nil
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

func (service *Service) monitor(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	run model.Run,
	stopped chan<- struct{},
) {
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
		err := service.repository.CommitObserveFinalization(ctx, model.ObserveCommand{
			Run: run, NowMS: now, LastMS: last,
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
