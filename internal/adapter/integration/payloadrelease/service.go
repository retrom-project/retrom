package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"retrom/internal/adapter/files/payloadfiles"
	"retrom/internal/foundation/cleanup"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	repository "retrom/internal/repo/payloadrelease"
	application "retrom/internal/service/payloadrelease"

	"retrom/internal/adapter/files/blobstore"
)

type Service struct {
	database    *sql.DB
	blobs       *blobstore.Store
	now         func() time.Time
	waitFor     func(context.Context, time.Duration) error
	worker      *application.Worker
	gc          *application.GCScheduler
	garbage     *application.GarbageCollector
	expirations *application.Expirations
	retirements *application.Retirements
	effects     *application.ReleaseEffects
}

type claimedJob struct {
	ID, ScopeID string
	ScopeType   ScopeType
	Attempt     int64
	Input       scheduleInput
	Work        payloadreleasemodel.Work
}

func New(database *sql.DB, blobs *blobstore.Store, now func() time.Time, retention time.Duration) (*Service, error) {
	service := &Service{database: database, blobs: blobs, now: now, waitFor: waitForContext}
	gc, err := application.NewGCScheduler(repository.NewGC(database), application.GCOptions{
		Now: now, Retention: retention, Wake: service.Signal,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize GC scheduling: %w", err)
	}
	service.gc = gc
	service.expirations = application.NewExpirations(repository.NewExpiration(database), gc, now)
	service.retirements = application.NewRetirements(repository.NewRetirement(database), now)
	if err := ValidateOwnershipRegistry(); err != nil {
		return nil, err
	}
	if err := validateLifecycleState(context.Background(), database); err != nil {
		return nil, err
	}
	service.worker = application.NewWorker(
		repository.NewWorker(database), releaseExecutor{service}, application.WorkerOptions{
			Now: now, Maintain: service.ReconcileGC, Report: func(err error) { cleanup.Error("payload worker", err) },
		})
	service.garbage = application.NewGarbageCollector(
		repository.NewGarbage(database), service.worker, payloadfiles.New(blobs),
	)
	service.effects = application.NewReleaseEffects(
		repository.NewReleaseEffects(database), service.worker, gc, releaseEffectWaiter{service}, now,
	)
	return service, nil
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("payloadrelease/wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (service *Service) Start() { service.worker.Start() }
func (service *Service) recoverInterruptedJobs(ctx context.Context) error {
	if err := service.worker.Recover(ctx); err != nil {
		return fmt.Errorf("recover release worker: %w", err)
	}
	return nil
}
func (service *Service) Close()  { service.worker.Close() }
func (service *Service) Signal() { service.worker.Signal() }

func (service *Service) ReconcileGC(ctx context.Context) error {
	if err := service.releaseSupersededBIOS(ctx); err != nil {
		return err
	}
	if err := service.releaseTerminalLaunches(ctx); err != nil {
		return err
	}
	if err := service.releaseExpiredReviewPreviews(ctx); err != nil {
		return err
	}
	if err := service.releaseExpiredProviderPayloads(ctx); err != nil {
		return err
	}
	return service.stageAllUnreferenced(ctx)
}

func (service *Service) RunOnce(ctx context.Context) (bool, error) {
	did, err := service.worker.RunOnce(ctx)
	if err != nil {
		return did, fmt.Errorf("run release worker: %w", err)
	}
	return did, nil
}

type releaseExecutor struct{ service *Service }

func (adapter releaseExecutor) Execute(ctx context.Context, unit payloadreleasemodel.Execution) error {
	return adapter.service.execute(ctx, claimedWork(unit))
}

func claimedWork(unit payloadreleasemodel.Execution) claimedJob {
	return claimedJob{
		ID: unit.Work.ID, ScopeID: unit.Work.Scope.ID, ScopeType: unit.Work.Scope.Type,
		Attempt: unit.Work.Attempt, Input: unit.Input, Work: unit.Work,
	}
}

func (service *Service) execute(ctx context.Context, job claimedJob) error {
	if job.Input.SchemaVersion != 1 || job.Input.Scope.ID != job.ScopeID || job.Input.Scope.Type != job.ScopeType {
		return releaseFailure("PAYLOAD_RELEASE_DATABASE_FAILED")
	}
	if job.Input.Kind == "BLOB_GC" {
		return service.executeBlobGC(ctx, job)
	}
	if job.Input.Kind != "PAYLOAD_RELEASE" {
		return releaseFailure("PAYLOAD_RELEASE_DATABASE_FAILED")
	}
	return service.releaseEffect(ctx, job)
}
