package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	wake     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
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
	return &Service{
		database: database, blobs: blobs, importer: importer, roots: roots, now: now, tags: tagging.New(
			tagpersistence.New(
				database,
			),
			now,
		),
		wake: make(chan struct{}, 1), stop: make(chan struct{}),
	}
}

func (service *Service) Start() {
	go service.runLoop()
	service.signal()
}

func (service *Service) Close() { service.stopOnce.Do(func() { close(service.stop) }) }

func (service *Service) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *Service) runLoop() {
	if err := service.recoverWork(context.Background()); err != nil {
		slog.Error("recover EmulationStation executions", "error", err)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-service.stop:
			return
		case <-service.wake:
		case <-ticker.C:
		}
		if err := service.recoverWork(context.Background()); err != nil {
			slog.Error("recover EmulationStation executions", "error", err)
		}
		_ = service.ExpirePlans(context.Background())
		for {
			unit, ok, err := service.claim(context.Background())
			if err != nil {
				slog.Error("claim EmulationStation execution", "error", err)
				break
			}
			if !ok {
				break
			}
			service.execute(context.Background(), unit)
		}
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
	if unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= service.now().UnixMilli() {
		service.fail(ctx, unit, "EMULATIONSTATION_EXECUTION_TIMEOUT", false)
		return
	}
	if unit.DeadlineAtMS > 0 {
		var cancel context.CancelFunc
		remaining := time.Duration(unit.DeadlineAtMS-service.now().UnixMilli()) * time.Millisecond
		ctx, cancel = context.WithTimeout(ctx, remaining)
		defer cancel()
	}
	heartbeatDone := make(chan struct{})
	go service.heartbeat(ctx, unit, heartbeatDone)
	defer close(heartbeatDone)
	root, ok := service.roots[unit.RootID]
	if !ok || root.digest != unit.RootDigest {
		service.fail(ctx, unit, "SERVER_IMPORT_ROOT_CHANGED", false)
		return
	}
	if unit.Kind == "SERVER_EMULATIONSTATION_SCAN" {
		service.executeScan(ctx, unit, root)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			service.fail(ctx, unit, "EMULATIONSTATION_EXECUTION_TIMEOUT", false)
		}
		return
	}
	service.executeImport(ctx, unit, root)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		service.fail(ctx, unit, "EMULATIONSTATION_EXECUTION_TIMEOUT", false)
	}
}

func (service *Service) heartbeat(ctx context.Context, unit work, done <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-service.stop:
			return
		case <-ticker.C:
			state, err := application.NewLeases(persistence.NewLeases(service.database), service.now).Renew(ctx, unit)
			if err != nil {
				slog.ErrorContext(ctx, "renew EmulationStation execution", "error", err)
				return
			}
			if state != application.LeaseActive {
				return
			}
		}
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
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
