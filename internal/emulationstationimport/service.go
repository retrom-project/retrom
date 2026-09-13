package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	persistence "retrom/internal/persistence/emulationstationimport"

	application "retrom/internal/service/emulationstationimport"

	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/blobstore"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/service/tagging"
)

var (
	ErrNotFound             = application.ErrNotFound
	ErrGamelistAbsent       = errors.New("EMULATIONSTATION_GAMELIST_NOT_FOUND")
	ErrNoValidGamelist      = errors.New("EMULATIONSTATION_NO_VALID_GAMELIST")
	ErrScanLimit            = errors.New("EMULATIONSTATION_SCAN_LIMIT_EXCEEDED")
	ErrMapping              = application.ErrMapping
	ErrVersionConflict      = application.ErrVersionConflict
	ErrNoSelection          = application.ErrNoSelection
	ErrSourceChanged        = application.ErrSourceChanged
	ErrMappingTargetChanged = application.ErrMappingTargetChanged
	ErrExpired              = application.ErrExpired
	ErrActive               = application.ErrActive
	ErrInvalid              = application.ErrInvalid
	ErrNotCancellable       = application.ErrNotCancellable
	ErrNotRetryable         = application.ErrNotRetryable
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

type work = application.Execution

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

func errorCode(err error) string {
	if errors.Is(err, serversource.ErrRootUnavailable) {
		return serversource.ErrRootUnavailable.Error()
	}
	candidates := []error{
		ErrGamelistAbsent,
		ErrNoValidGamelist,
		ErrScanLimit,
		ErrSourceChanged,
		ErrMappingTargetChanged,
		ErrMapping,
		ErrNoSelection,
		ErrExpired,
		ErrActive,
		ErrInvalid,
	}
	for _, candidate := range candidates {
		if errors.Is(err, candidate) {
			return candidate.Error()
		}
	}
	if err != nil && strings.HasPrefix(err.Error(), "EMULATIONSTATION_") {
		return strings.SplitN(err.Error(), ":", 2)[0]
	}
	return "INTERNAL_ERROR"
}
