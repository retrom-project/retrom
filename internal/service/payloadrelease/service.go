package payloadrelease

import (
	"context"
	"fmt"
	"time"
)

type Dependencies struct {
	Worker    WorkerRepository
	GC        GCRepository
	Garbage   GarbageRepository
	Effects   map[ScopeType]EffectRepository
	Providers ProviderExpirationRepository
	Previews  PreviewExpirationRepository
	BIOS      BIOSRetirementRepository
	Launches  LaunchRetirementRepository
	Files     GarbageFiles
	Waiter    EffectWaiter
}

type Options struct {
	Now    func() time.Time
	NewID  func() (string, error)
	Report func(error)
}

type Service struct {
	worker      *Worker
	gc          *GCScheduler
	garbage     *GarbageCollector
	effects     map[ScopeType]*ReleaseEffects
	expirations *Expirations
	retirements *Retirements
}

func New(_ context.Context, dependencies Dependencies, options Options) (*Service, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	service := &Service{}
	gc, err := NewGCScheduler(dependencies.GC, GCOptions{
		Now: options.Now, NewID: options.NewID, Wake: service.Signal,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize payload GC: %w", err)
	}
	service.gc = gc
	service.expirations = NewExpirations(dependencies.Providers, dependencies.Previews, gc, options.Now)
	service.retirements = NewRetirements(dependencies.BIOS, dependencies.Launches, options.Now)
	service.worker = NewWorker(dependencies.Worker, service, WorkerOptions{
		Now: options.Now, NewID: options.NewID, Maintain: service.ReconcileGC, Report: options.Report,
	})
	service.garbage = NewGarbageCollector(dependencies.Garbage, service.worker, dependencies.Files)
	service.effects = make(map[ScopeType]*ReleaseEffects, len(dependencies.Effects))
	for kind, repository := range dependencies.Effects {
		service.effects[kind] = NewReleaseEffects(repository, service.worker, dependencies.Waiter, options.Now)
	}
	return service, nil
}

func (service *Service) Execute(ctx context.Context, unit Execution) error {
	if unit.Input.SchemaVersion != 1 || unit.Input.Scope != unit.Work.Scope || unit.Input.Kind != unit.Work.Kind {
		return effectFailure("PAYLOAD_RELEASE_DATABASE_FAILED", ErrInputInvalid)
	}
	switch unit.Input.Kind {
	case "BLOB_GC":
		return service.garbage.Execute(ctx, unit)
	case "PAYLOAD_RELEASE":
		handler, ok := service.effects[unit.Work.Scope.Type]
		if !ok {
			return effectFailure("PAYLOAD_RELEASE_DATABASE_FAILED", ErrScopeInvalid)
		}
		return handler.Execute(ctx, unit)
	default:
		return effectFailure("PAYLOAD_RELEASE_DATABASE_FAILED", ErrInputInvalid)
	}
}

func (service *Service) ReconcileGC(ctx context.Context) error {
	if err := service.retirements.BIOS(ctx); err != nil {
		return err
	}
	if err := service.retirements.Launches(ctx); err != nil {
		return err
	}
	if err := service.expirations.Previews(ctx); err != nil {
		return err
	}
	if err := service.expirations.Providers(ctx); err != nil {
		return err
	}
	return service.gc.Reconcile(ctx)
}
