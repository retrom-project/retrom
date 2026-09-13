package payloadrelease

import (
	"context"
	"fmt"
	"time"
)

type Dependencies struct {
	Lifecycle  LifecycleRepository
	Worker     WorkerRepository
	GC         GCRepository
	Garbage    GarbageRepository
	Effects    EffectRepository
	Expiration ExpirationRepository
	Retirement RetirementRepository
	Impact     ImpactReader
	Files      GarbageFiles
	Waiter     EffectWaiter
}

type Options struct {
	Now       func() time.Time
	NewID     func() (string, error)
	Retention time.Duration
	Report    func(error)
}

type Service struct {
	worker      *Worker
	gc          *GCScheduler
	garbage     *GarbageCollector
	effects     *ReleaseEffects
	expirations *Expirations
	retirements *Retirements
	impact      *ImpactQueries
}

func New(ctx context.Context, dependencies Dependencies, options Options) (*Service, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	service := &Service{}
	gc, err := NewGCScheduler(dependencies.GC, GCOptions{
		Now: options.Now, Retention: options.Retention, NewID: options.NewID, Wake: service.Signal,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize payload GC: %w", err)
	}
	if err := NewLifecycleVerifier(dependencies.Lifecycle).Validate(ctx); err != nil {
		return nil, err
	}
	service.gc = gc
	service.expirations = NewExpirations(dependencies.Expiration, gc, options.Now)
	service.retirements = NewRetirements(dependencies.Retirement, options.Now)
	service.impact = NewImpactQueries(dependencies.Impact)
	service.worker = NewWorker(dependencies.Worker, service, WorkerOptions{
		Now: options.Now, NewID: options.NewID, Maintain: service.ReconcileGC, Report: options.Report,
	})
	service.garbage = NewGarbageCollector(dependencies.Garbage, service.worker, dependencies.Files)
	service.effects = NewReleaseEffects(dependencies.Effects, service.worker, gc, dependencies.Waiter, options.Now)
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
		return service.effects.Execute(ctx, unit)
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
