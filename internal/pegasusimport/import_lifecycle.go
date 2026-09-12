package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) StartImport(ctx context.Context, id string, version int64) (Summary, error) {
	actorID := ""
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	starter := application.NewStarter(
		repository.NewStarter(service.database),
		creationSourceSelector{service},
		service.now,
	)
	result, queued, err := starter.Start(ctx, id, version, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("start Pegasus import: %w", err)
	}
	if queued {
		service.signal()
	}
	return result, nil
}

func (service *Service) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actorID string,
) (Summary, bool, error) {
	control := application.NewWorkflowControl(repository.NewWorkflowControl(service.database), service.now)
	result, pending, err := control.Cancel(ctx, id, version, reason, actorID)
	if err != nil {
		return Summary{}, false, fmt.Errorf("cancel Pegasus import: %w", err)
	}
	service.signal()
	return result, pending, nil
}

func (service *Service) CancelJob(
	ctx context.Context,
	request application.JobCancellationRequest,
) (application.JobCancellationResult, bool, error) {
	control := application.NewWorkflowControl(repository.NewWorkflowControl(service.database), service.now)
	result, pending, err := control.CancelJob(ctx, request)
	if err != nil {
		return application.JobCancellationResult{}, false, fmt.Errorf("cancel Pegasus job: %w", err)
	}
	service.signal()
	return result, pending, nil
}

func (service *Service) Delete(ctx context.Context, id string, version int64) error {
	actorID := ""
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	lifecycle := application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now)
	if err := lifecycle.Delete(ctx, id, version, actorID); err != nil {
		return fmt.Errorf("delete Pegasus import: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	lifecycle := application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now)
	if err := lifecycle.Expire(ctx); err != nil {
		return fmt.Errorf("expire Pegasus imports: %w", err)
	}
	return nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	control := application.NewWorkflowControl(repository.NewWorkflowControl(service.database), service.now)
	result, err := control.Retry(ctx, id, version, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("retry Pegasus import: %w", err)
	}
	service.signal()
	return result, nil
}
