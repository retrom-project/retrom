package cleanupjobs

import (
	"context"
	"fmt"
	"time"

	gamecleanup "retrom/internal/service/gamecontent/payloadpolicy"
	importcleanup "retrom/internal/service/libraryimport/payloadpolicy"
	sourcecleanup "retrom/internal/service/sourceimport/payloadpolicy"
	uploadcleanup "retrom/internal/service/uploads/payloadpolicy"

	biosretirement "retrom/internal/service/firmware/retirement"
	launchretirement "retrom/internal/service/launch/retirement"
	previewretention "retrom/internal/service/libraryimport/previewretention"
	providerretention "retrom/internal/service/metadatascrape/providerretention"

	workerrepo "retrom/internal/persistence/cleanupjobs"

	dbapi "retrom/internal/database"

	"retrom/internal/cleanup"
	physicalfiles "retrom/internal/filedeletion"
	"retrom/internal/filestore"
	"retrom/internal/persistence/filedeletion"
	"retrom/internal/persistence/firmware/payloadbios"
	"retrom/internal/persistence/gamecontent/gamerelease"
	"retrom/internal/persistence/launch/payloadlaunch"
	"retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/libraryimport/payloadpreview"
	"retrom/internal/persistence/metadatascrape/payloadprovider"

	"retrom/internal/persistence/sourceimport/sourcerelease"
	"retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/cleanupjobs"
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
	blobs *filestore.Store,
	now func() time.Time,
) (*Service, error) {
	files := physicalfiles.New(blobs)
	service, err := application.New(
		ctx,
		application.Dependencies{
			Worker: workerrepo.NewWorker(database),

			Effects: map[application.ScopeType]application.EffectBinding{
				application.ScopeGame:             gamecleanup.Cleanup(gamerelease.NewEffects(database), files),
				application.ScopeImportItem:       {Repository: itemrelease.NewEffects(database), Apply: importcleanup.Release},
				application.ScopeImportJob:        {Repository: itemrelease.NewEffects(database), Apply: importcleanup.Release},
				application.ScopeSourceImportItem: {Repository: sourcerelease.NewEffects(database), Apply: sourcecleanup.Release},
				application.ScopeUploadConsumption: {
					Repository: payloadpurge.NewEffects(database),
					Apply:      uploadcleanup.ConsumptionCleanup,
				},
			},
			Maintenance: func(deletion application.DeletionStager) []func(context.Context) error {
				return []func(context.Context) error{
					scratchMaintenance(blobs, database, now),
					biosretirement.New(payloadbios.New(database), now).BIOS,
					launchretirement.New(payloadlaunch.New(database), now).Launches,
					previewretention.New(payloadpreview.New(database), deletion, now).Previews,
					providerretention.New(payloadprovider.New(database), deletion, now).Providers,
				}
			},

			Files: files,
		},
		application.Options{
			Now:    now,
			Report: func(err error) { cleanup.Error("payload worker", err) },
		},
	)
	if err != nil {
		return nil, fmt.Errorf("initialize payload release service: %w", err)
	}
	return &Service{Service: service, database: database}, nil
}

// StageCandidates keeps a caller-owned transaction and the DeletionQueue application
// policy together.  It is intentionally a composition concern: the SQL
// transaction is never exposed to the application service.
func (service *Service) StageCandidates(ctx context.Context, transaction dbapi.Tx, ids []string) error {
	if err := service.StageInScope(ctx, filedeletion.Bind(transaction), ids); err != nil {
		return fmt.Errorf("stage payload release candidates: %w", err)
	}
	return nil
}

// ScheduleConsumption queues release of an upload consumption in a caller-owned
// transaction. The application scheduler is deliberately bound here so HTTP
// adapters do not import persistence or the worker implementation.
func (service *Service) ScheduleConsumption(
	ctx context.Context, transaction dbapi.Tx, consumptionID string, now int64,
) (string, error) {
	jobID, err := uploadcleanup.Consumption(ctx, application.NewScheduler(nil),
		payloadpurge.BindScheduling(transaction), consumptionID, now,
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
	jobID, err := gamecleanup.DeleteGame(ctx, application.NewScheduler(nil),
		gamerelease.BindScheduling(transaction), gameID, version, now)
	if err != nil {
		return "", fmt.Errorf("schedule game payload release: %w", err)
	}
	return jobID, nil
}
