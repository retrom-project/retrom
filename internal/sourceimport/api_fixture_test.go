package sourceimport

import (
	"context"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/sourceimport"
	application "retrom/internal/service/sourceimport"
)

func (service *Service) application() *application.Service {
	return application.New(application.ServiceDependencies{
		Queries: service.queries(), Creation: application.NewCreation(repository.NewCreation(service.database), service.sources(), service.now),
		Mappings:  application.NewMappings(repository.NewMappings(service.database), service.tags, service.now),
		Starter:   application.NewStarter(repository.NewStarter(service.database), service.sources(), service.now),
		Control:   application.NewWorkflowControl(repository.NewWorkflowControl(service.database), service.now),
		Lifecycle: application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now), Worker: service.backgroundWorker(),
	})
}

func fixtureActor(ctx context.Context) string {
	principal, _ := authn.PrincipalFromContext(ctx)
	return principal.UserID
}

func (service *Service) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	return service.application().Create(ctx, request, actorID)
}

func (service *Service) StartImport(ctx context.Context, id string, version int64) (Summary, error) {
	return service.application().StartImport(ctx, id, version, fixtureActor(ctx))
}

func (service *Service) UpdateMappings(ctx context.Context, id string, version int64, mappings []Mapping) (Summary, error) {
	return service.application().UpdateMappings(ctx, id, version, mappings, fixtureActor(ctx))
}

func (service *Service) Delete(ctx context.Context, id string, version int64) error {
	return service.application().Delete(ctx, id, version, fixtureActor(ctx))
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	return application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now).Expire(ctx)
}

func (service *Service) Cancel(ctx context.Context, id string, version int64, reason, actorID string) (Summary, bool, error) {
	return service.application().Cancel(ctx, id, version, reason, actorID)
}

func (service *Service) CancelJob(ctx context.Context, request application.JobCancellationRequest) (application.JobCancellationResult, bool, error) {
	return service.application().CancelJob(ctx, request)
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	return service.application().Retry(ctx, id, version, actorID)
}
