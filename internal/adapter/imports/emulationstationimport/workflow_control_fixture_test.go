package emulationstationimport

import (
	"context"
	"fmt"

	repository "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
)

func (service *Service) workflowControl() *application.WorkflowControl {
	return application.NewWorkflowControl(
		repository.NewWorkflowControl(service.database, payloadreleaseservice.NewScheduler(nil)),
		service.sources(),
		service.now,
	)
}

func (service *Service) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actor string,
) (Summary, bool, error) {
	result, pending, err := service.workflowControl().Cancel(ctx, id, version, reason, actor)
	if err != nil {
		return Summary{}, false, fmt.Errorf("cancel EmulationStation import: %w", err)
	}
	service.signal()
	return result, pending, nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actor string) (Summary, error) {
	result, err := service.workflowControl().Retry(ctx, id, version, actor)
	if err != nil {
		return Summary{}, fmt.Errorf("retry EmulationStation import: %w", err)
	}
	service.signal()
	return result, nil
}

type (
	JobCancellationRequest = application.JobCancellationRequest
	JobCancellationResult  = application.JobCancellationResult
)

func (service *Service) CancelJob(
	ctx context.Context,
	request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	result, pending, err := service.workflowControl().CancelJob(ctx, request)
	if err != nil {
		return JobCancellationResult{}, false, fmt.Errorf("cancel EmulationStation job: %w", err)
	}
	service.signal()
	return result, pending, nil
}
