package serverimport

import (
	"context"
	"errors"
	"time"
)

func (service *Service) progress(ctx context.Context, unit work, phase string, current, total int64) {
	if unit.progress != nil && !unit.progress.allow(phase, current, total, service.now()) {
		return
	}
	service.workerError("progress", service.leases.Progress(ctx, unit, phase, current, total))
}

func (service *Service) completeItem(
	ctx context.Context,
	unit work,
	requirementID, state string,
	candidate *evaluatedCandidate,
	code string,
) {
	service.workerError("complete item", service.outcomes.CompleteItem(ctx, unit, requirementID, state, candidate, code))
}

func (service *Service) finishTask(ctx context.Context, unit work) {
	service.workerError("finish", service.outcomes.Finish(ctx, unit))
}

func (service *Service) failTask(ctx context.Context, unit work, code string) {
	retryAt, err := service.outcomes.Fail(ctx, unit, code)
	service.workerError("fail", err)
	if err == nil && retryAt > 0 {
		time.AfterFunc(time.Duration(retryAt-service.now().UnixMilli())*time.Millisecond, service.signal)
	}
}

func (service *Service) cancelTask(ctx context.Context, unit work) {
	if ctx.Err() != nil {
		if !errors.Is(context.Cause(ctx), ErrWorkerCancelled) {
			return
		}
		ctx = context.WithoutCancel(ctx)
	}
	service.workerError("cancel", service.outcomes.Cancel(ctx, unit))
}

func (service *Service) cancelRequested(ctx context.Context, _ work) bool {
	return ctx.Err() != nil
}
