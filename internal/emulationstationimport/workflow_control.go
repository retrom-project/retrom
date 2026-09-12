package emulationstationimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) workflowControl() *application.WorkflowControl {
	return application.NewWorkflowControl(
		repository.NewWorkflowControl(service.database),
		frozenSources{creationSourceSelector{roots: service.roots}},
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
