package pegasusimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/pegasusimport"
)

type (
	PlanCreator interface {
		Create(context.Context, model.CreateRequest, string) (model.Summary, error)
	}
	ImportStarter interface {
		Start(context.Context, string, int64, string) (model.Summary, bool, error)
	}
	PlanController interface {
		Cancel(context.Context, string, int64, string, string) (model.Summary, bool, error)
		CancelJob(context.Context, JobCancellationRequest) (JobCancellationResult, bool, error)
		Retry(context.Context, string, int64, string) (model.Summary, error)
	}
)

type (
	ServiceWorker interface {
		Start()
		Close()
		Signal()
	}
	ServiceDependencies struct {
		Queries   *Queries
		Creation  PlanCreator
		Mappings  *Mappings
		Starter   ImportStarter
		Control   PlanController
		Lifecycle *PlanLifecycle
		Worker    ServiceWorker
	}
)

// Service is the process-facing application API. Its dependencies are assembled once at startup.
type Service struct{ dependencies ServiceDependencies }

func New(dependencies ServiceDependencies) *Service { return &Service{dependencies: dependencies} }
func (service *Service) Start()                     { service.dependencies.Worker.Start() }
func (service *Service) Close()                     { service.dependencies.Worker.Close() }
func (service *Service) Get(ctx context.Context, id string) (model.Summary, error) {
	return service.dependencies.Queries.Get(ctx, id)
}

func (service *Service) List(ctx context.Context, query model.ListQuery) ([]model.Summary, error) {
	return service.dependencies.Queries.List(ctx, query)
}

func (service *Service) Collections(ctx context.Context, query model.CollectionQuery) ([]model.Collection, error) {
	return service.dependencies.Queries.Collections(ctx, query)
}

func (service *Service) Items(ctx context.Context, query model.ItemQuery) ([]model.Item, error) {
	return service.dependencies.Queries.Items(ctx, query)
}

func (service *Service) Create(ctx context.Context, request model.CreateRequest, actorID string) (
	model.Summary,
	error,
) {
	result, err := service.dependencies.Creation.Create(ctx, request, actorID)
	if err != nil {
		return model.Summary{}, fmt.Errorf("create Pegasus import: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, nil
}

func (service *Service) StartImport(ctx context.Context, id string, version int64, actorID string) (
	model.Summary,
	error,
) {
	result, queued, err := service.dependencies.Starter.Start(ctx, id, version, actorID)
	if err != nil {
		return model.Summary{}, fmt.Errorf("start Pegasus import: %w", err)
	}
	if queued {
		service.dependencies.Worker.Signal()
	}
	return result, nil
}

func (service *Service) UpdateMappings(
	ctx context.Context, id string, version int64, mappings []model.Mapping, actorID string,
) (model.Summary, error) {
	return service.dependencies.Mappings.Update(ctx, id, version, mappings, actorID)
}

func (service *Service) Delete(ctx context.Context, id string, version int64, actorID string) error {
	return service.dependencies.Lifecycle.Delete(ctx, id, version, actorID)
}

func (service *Service) Cancel(
	ctx context.Context, id string, version int64, reason, actorID string,
) (model.Summary, bool, error) {
	result, pending, err := service.dependencies.Control.Cancel(ctx, id, version, reason, actorID)
	if err != nil {
		return model.Summary{}, false, fmt.Errorf("cancel Pegasus import: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, pending, nil
}

func (service *Service) CancelJob(
	ctx context.Context, request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	result, pending, err := service.dependencies.Control.CancelJob(ctx, request)
	if err != nil {
		return JobCancellationResult{}, false, fmt.Errorf("cancel Pegasus job: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, pending, nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actorID string) (model.Summary, error) {
	result, err := service.dependencies.Control.Retry(ctx, id, version, actorID)
	if err != nil {
		return model.Summary{}, fmt.Errorf("retry Pegasus import: %w", err)
	}
	service.dependencies.Worker.Signal()
	return result, nil
}
