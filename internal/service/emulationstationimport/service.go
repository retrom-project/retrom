package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/emulationstationimport"
)

type PlanCreator interface {
	Create(context.Context, model.CreateRequest, string) (model.Summary, error)
}
type ImportStarter interface {
	Start(context.Context, string, int64, string) (model.Summary, bool, error)
}
type PlanController interface {
	Cancel(context.Context, string, int64, string, string) (model.Summary, bool, error)
	CancelJob(context.Context, JobCancellationRequest) (JobCancellationResult, bool, error)
	Retry(context.Context, string, int64, string) (model.Summary, error)
}
type PlanAdministration interface {
	Delete(context.Context, string, int64, string) error
	Expire(context.Context) error
}
type ServiceWorker interface {
	Start()
	Close()
	Signal()
}
type ServiceDependencies struct {
	Queries   *Queries
	Creation  PlanCreator
	Mappings  *Mappings
	Starter   ImportStarter
	Control   PlanController
	Lifecycle PlanAdministration
	Worker    ServiceWorker
}
type Service struct{ dependencies ServiceDependencies }

func New(dependencies ServiceDependencies) *Service { return &Service{dependencies: dependencies} }
func (service *Service) Start()                     { service.dependencies.Worker.Start() }
func (service *Service) Close()                     { service.dependencies.Worker.Close() }
func (service *Service) Create(ctx context.Context, request model.CreateRequest, actor string) (model.Summary, error) {
	result, err := service.dependencies.Creation.Create(ctx, request, actor)
	if err != nil {
		return model.Summary{}, fmt.Errorf("create EmulationStation scan plan: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, nil
}

func (service *Service) StartImport(ctx context.Context, id string, version int64) (model.Summary, error) {
	result, queued, err := service.dependencies.Starter.Start(ctx, id, version, contextActor(ctx))
	if err != nil {
		return model.Summary{}, fmt.Errorf("start EmulationStation import: %w", err)
	}
	if queued {
		service.dependencies.Worker.Signal()
	}
	return result, nil
}

func (service *Service) UpdateMappings(
	ctx context.Context,
	id string,
	version int64,
	mappings []model.Mapping,
) (model.Summary, error) {
	return service.dependencies.Mappings.Update(ctx, id, version, mappings, contextActor(ctx))
}

func (service *Service) Delete(ctx context.Context, id string, version int64) error {
	if err := service.dependencies.Lifecycle.Delete(ctx, id, version, contextActor(ctx)); err != nil {
		return fmt.Errorf("delete EmulationStation plan: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	if err := service.dependencies.Lifecycle.Expire(ctx); err != nil {
		return fmt.Errorf("expire EmulationStation plans: %w", err)
	}
	return nil
}

func contextActor(ctx context.Context) string {
	if principal, found := authn.PrincipalFromContext(ctx); found {
		return principal.UserID
	}
	return ""
}
