package emulationstationimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/emulationstationimport"
)

func (service *Service) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actor string,
) (model.Summary, bool, error) {
	result, pending, err := service.dependencies.Control.Cancel(ctx, id, version, reason, actor)
	if err != nil {
		return model.Summary{}, false, fmt.Errorf("cancel EmulationStation import: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, pending, nil
}

func (service *Service) CancelJob(
	ctx context.Context,
	request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	result, pending, err := service.dependencies.Control.CancelJob(ctx, request)
	if err != nil {
		return JobCancellationResult{}, false, fmt.Errorf("cancel EmulationStation job: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, pending, nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actor string) (model.Summary, error) {
	result, err := service.dependencies.Control.Retry(ctx, id, version, actor)
	if err != nil {
		return model.Summary{}, fmt.Errorf("retry EmulationStation import: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, nil
}
