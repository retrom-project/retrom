package serverimport

import (
	"context"
	"time"

	model "retrom/internal/model/serverimport"
)

func (service *Service) progress(ctx context.Context, unit model.Work, phase string, current, total int64) {
	service.workerError("progress", service.leases.Progress(ctx, unit, phase, current, total))
}

func (service *Service) completeItem(
	ctx context.Context,
	unit model.Work,
	requirementID, state string,
	candidate *EvaluatedCandidate,
	code string,
) {
	service.workerError("complete item", service.outcomes.CompleteItem(ctx, unit, requirementID, state, candidate, code))
}

func (service *Service) finishTask(ctx context.Context, unit model.Work) {
	service.workerError("finish", service.outcomes.Finish(ctx, unit))
}

func (service *Service) failTask(ctx context.Context, unit model.Work, code string) {
	retryAt, err := service.outcomes.Fail(ctx, unit, code)
	service.workerError("fail", err)
	if err == nil && retryAt > 0 {
		time.AfterFunc(time.Duration(retryAt-service.now().UnixMilli())*time.Millisecond, service.signal)
	}
}

func (service *Service) cancelTask(ctx context.Context, unit model.Work) {
	service.workerError("cancel", service.outcomes.Cancel(ctx, unit))
}

func (service *Service) cancelRequested(ctx context.Context, unit model.Work) bool {
	return service.pollCancellation(ctx, unit)
}

func (service *Service) pollCancellation(ctx context.Context, unit model.Work) bool {
	err := service.leases.Heartbeat(ctx, unit)
	service.workerError("poll cancellation", err)
	return err != nil
}
