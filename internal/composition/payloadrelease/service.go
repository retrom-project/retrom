package payloadrelease

import (
	"context"
	"fmt"
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/payloadfiles"
	"retrom/internal/persistence/blobgc"
	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

// Service is the process composition facade for payload release.  The
// application service remains database agnostic; this thin type only exposes
// transaction-bound adapters needed by HTTP/composition callers.
type Service struct {
	*application.Service
	database dbapi.DB
}

func New(
	ctx context.Context,
	database dbapi.DB,
	blobs *blobstore.Store,
	now func() time.Time,
) (*Service, error) {
	files := payloadfiles.New(blobs)
	service, err := application.New(ctx, application.Dependencies{
		Worker: repository.NewWorker(database),
		GC:     blobgc.NewGC(database, repository.BindWorker), Garbage: blobgc.NewGarbage(database, repository.BindWorker),
		Effects: repository.NewReleaseEffects(database), Expiration: repository.NewExpiration(database),
		Retirement: repository.NewRetirement(database),
		Files:      files, Waiter: files,
	}, application.Options{
		Now:    now,
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
func (service *Service) StageCandidates(ctx context.Context, transaction dbapi.Tx, ids []string) error {
	if err := service.StageInScope(ctx, repository.BindGC(transaction), ids); err != nil {
		return fmt.Errorf("stage payload release candidates: %w", err)
	}
	return nil
}

// ScheduleConsumption queues release of an upload consumption in a caller-owned
// transaction. The application scheduler is deliberately bound here so HTTP
// adapters do not import persistence or the legacy payload package.
func (service *Service) ScheduleConsumption(
	ctx context.Context, transaction dbapi.Tx, consumptionID string, now int64,
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
	ctx context.Context, transaction dbapi.Tx, gameID string, version, now int64,
) (string, error) {
	jobID, err := application.NewScheduler(nil).DeleteGame(
		ctx, repository.BindScheduling(transaction), gameID, version, now,
	)
	if err != nil {
		return "", fmt.Errorf("schedule game payload release: %w", err)
	}
	return jobID, nil
}
