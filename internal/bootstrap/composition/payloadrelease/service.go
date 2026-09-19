package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/payloadfiles"
	"retrom/internal/foundation/cleanup"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	repository "retrom/internal/repo/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

// Service is the process composition facade for payload release.  The
// application service remains database agnostic; this thin type only exposes
// transaction-bound adapters needed by HTTP/composition callers.
type Service struct {
	*application.Service
	database *sql.DB
}

func New(
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	now func() time.Time,
	retention time.Duration,
) (*Service, error) {
	files := payloadfiles.New(blobs)
	service, err := application.New(ctx, application.Dependencies{
		Lifecycle: repository.NewLifecycle(database), Worker: repository.NewWorker(database),
		GC: repository.NewGC(database), Garbage: repository.NewGarbage(database),
		Effects: repository.NewReleaseEffects(database), Expiration: repository.NewExpiration(database),
		Retirement: repository.NewRetirement(database), Impact: repository.NewImpactQueries(database),
		Files: files, Waiter: files,
	}, application.Options{
		Now: now, Retention: retention,
		Report: func(err error) { cleanup.Error("payload worker", err) },
	})
	if err != nil {
		return nil, fmt.Errorf("initialize payload release service: %w", err)
	}
	return &Service{Service: service, database: database}, nil
}

// StageCandidates keeps a caller-owned transaction and the GC application
// policy together.  It is intentionally a composition concern: the SQL
// transaction is never exposed to the application service.
func (service *Service) StageCandidates(ctx context.Context, transaction *sql.Tx, ids []string) error {
	if err := service.StageInScope(ctx, repository.BindGC(transaction), ids); err != nil {
		return fmt.Errorf("stage payload release candidates: %w", err)
	}
	return nil
}

// ScheduleConsumption queues release of an upload consumption in a caller-owned
// transaction. The application scheduler is deliberately bound here so HTTP
// adapters do not import persistence or the legacy payload package.
func (service *Service) ScheduleConsumption(
	ctx context.Context, transaction *sql.Tx, consumptionID string, now int64,
) (string, error) {
	jobID, err := application.NewScheduler(nil).Consumption(
		ctx, repository.BindScheduling(transaction), consumptionID, now,
	)
	if err != nil {
		return "", fmt.Errorf("schedule payload consumption release: %w", err)
	}
	return jobID, nil
}

// ScheduleGameDeletion queues release of a game's payload in a caller-owned
// transaction while keeping the transaction boundary in composition.
func (service *Service) ScheduleGameDeletion(
	ctx context.Context, transaction *sql.Tx, gameID string, version, now int64,
) (string, error) {
	jobID, err := application.NewScheduler(nil).DeleteGame(
		ctx, repository.BindScheduling(transaction), gameID, version, now,
	)
	if err != nil {
		return "", fmt.Errorf("schedule game payload release: %w", err)
	}
	return jobID, nil
}

// GameDeleteImpactTx reads the deletion impact from a caller-owned transaction.
func (service *Service) GameDeleteImpactTx(
	ctx context.Context, transaction *sql.Tx, gameID string,
) (payloadreleasemodel.GameImpact, error) {
	impact, err := payloadreleasemodel.NewImpactQueries(repository.BindImpact(transaction)).Game(ctx, gameID)
	if err != nil {
		return payloadreleasemodel.GameImpact{}, fmt.Errorf("read game payload impact: %w", err)
	}
	return impact, nil
}
