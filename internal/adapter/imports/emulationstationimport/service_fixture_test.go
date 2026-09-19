package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"

	application "retrom/internal/service/emulationstationimport"

	tagpersistence "retrom/internal/repo/tagging"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/integration/libraryimport"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/service/tagging"
)

var (
	ErrNotFound             = emulationstationimportmodel.ErrNotFound
	ErrGamelistAbsent       = application.ErrGamelistAbsent
	ErrNoValidGamelist      = application.ErrNoValidGamelist
	ErrScanLimit            = application.ErrScanLimit
	ErrMapping              = emulationstationimportmodel.ErrMapping
	ErrVersionConflict      = emulationstationimportmodel.ErrVersionConflict
	ErrNoSelection          = emulationstationimportmodel.ErrNoSelection
	ErrSourceChanged        = emulationstationimportmodel.ErrSourceChanged
	ErrMappingTargetChanged = emulationstationimportmodel.ErrMappingTargetChanged
	ErrExpired              = emulationstationimportmodel.ErrExpired
	ErrActive               = emulationstationimportmodel.ErrActive
	ErrInvalid              = emulationstationimportmodel.ErrInvalid
	ErrNotCancellable       = emulationstationimportmodel.ErrNotCancellable
	ErrNotRetryable         = emulationstationimportmodel.ErrNotRetryable
	errItemStateChanged     = errors.New("item state changed")
)

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	importer *libraryimport.Service
	roots    map[string]Root
	now      func() time.Time
	tags     *tagging.Service
	worker   *application.Worker
}

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	importer *libraryimport.Service,
	credentials *retromruntime.Credentials,
	configured []serversource.Root,
	now func() time.Time,
) *Service {
	roots := make(map[string]Root, len(configured))
	for _, configuredRoot := range configured {
		digest := credentials.ServerImportRootDigest(configuredRoot.ID, configuredRoot.Path)
		roots[configuredRoot.ID] = Root{
			ID:     configuredRoot.ID,
			Label:  configuredRoot.Label,
			path:   configuredRoot.Path,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	service := &Service{
		database: database, blobs: blobs, importer: importer, roots: roots, now: now, tags: tagging.New(
			tagpersistence.New(
				database,
			),
			now,
		),
	}
	service.worker = service.newWorker()
	return service
}

func (service *Service) Start() { service.worker.Start() }
func (service *Service) Close() { service.worker.Close() }
func (service *Service) signal() {
	if service.worker != nil {
		service.worker.Signal()
	}
}

func (service *Service) recoverWork(ctx context.Context) error {
	if err := application.NewRecovery(persistence.NewRecovery(service.database), service.now).Recover(ctx); err != nil {
		return fmt.Errorf("recover EmulationStation work: %w", err)
	}
	return nil
}

type work = emulationstationimportmodel.Execution

func (service *Service) claim(ctx context.Context) (work, bool, error) {
	unit, found, err := application.NewLeases(persistence.NewLeases(service.database), service.now).Claim(ctx)
	if err != nil {
		return work{}, false, fmt.Errorf("claim EmulationStation work: %w", err)
	}
	return unit, found, nil
}

func (service *Service) execute(ctx context.Context, unit work) {
	service.worker.Run(ctx, unit)
}
