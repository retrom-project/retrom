package cleanupjobs

import (
	"context"
	"time"
)

type Dependencies struct {
	Worker      WorkerRepository
	Effects     map[ScopeType]EffectBinding
	Files       FileDeletionFiles
	Maintenance func(DeletionStager) []func(context.Context) error
}

type Options struct {
	Now    func() time.Time
	NewID  func() (string, error)
	Report func(error)
}

type Service struct {
	worker       *Worker
	deletion     *DeletionScheduler
	fileDeletion *FileDeletionCollector
	effects      map[ScopeType]*ReleaseEffects
	maintenance  []func(context.Context) error
}

func New(_ context.Context, dependencies Dependencies, options Options) (*Service, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	service := &Service{}
	deletion := NewDeletionScheduler(DeletionOptions{Now: options.Now})
	service.deletion = deletion
	if dependencies.Maintenance != nil {
		service.maintenance = dependencies.Maintenance(deletion)
	}
	service.worker = NewWorker(dependencies.Worker, service, WorkerOptions{
		Now: options.Now, NewID: options.NewID, Maintain: service.ReconcileDeletion, Report: options.Report,
	})
	service.fileDeletion = NewFileDeletionCollector(dependencies.Worker, service.worker, dependencies.Files)
	service.effects = make(map[ScopeType]*ReleaseEffects, len(dependencies.Effects))
	for kind, binding := range dependencies.Effects {
		service.effects[kind] = NewReleaseEffects(binding, service.worker, options.Now)
	}
	return service, nil
}

func (service *Service) Execute(ctx context.Context, unit Execution) error {
	if unit.Input.SchemaVersion != 1 || unit.Input.Scope != unit.Work.Scope || unit.Input.Kind != unit.Work.Kind {
		return Failure("OWNER_CLEANUP_DATABASE_FAILED", ErrInputInvalid)
	}
	switch unit.Input.Kind {
	case "PATH_DELETE":
		return service.fileDeletion.Execute(ctx, unit)
	case "OWNER_CLEANUP":
		handler, ok := service.effects[unit.Work.Scope.Type]
		if !ok {
			return Failure("OWNER_CLEANUP_DATABASE_FAILED", ErrScopeInvalid)
		}
		return handler.Execute(ctx, unit)
	default:
		return Failure("OWNER_CLEANUP_DATABASE_FAILED", ErrInputInvalid)
	}
}

func (service *Service) ReconcileDeletion(ctx context.Context) error {
	for _, maintain := range service.maintenance {
		if err := maintain(ctx); err != nil {
			return err
		}
	}
	return nil
}
