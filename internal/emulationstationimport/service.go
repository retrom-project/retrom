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

	"retrom/internal/dbexec"
	application "retrom/internal/service/emulationstationimport"

	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/persistence/recordstore"

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
	_ = service.recoverWork(context.Background())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-service.stop:
			return
		case <-service.wake:
		case <-ticker.C:
		}
		_ = service.recoverWork(context.Background())
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

const recoverTerminalImportsSQLAssignments = `state='FAILED',phase=NULL,
last_error_code=(
 SELECT job.error_code
 FROM jobs job
 WHERE job.scope_type='EMULATIONSTATION_IMPORT'
 AND job.scope_id=emulationstation_imports.id
 AND job.state='FAILED'
 ORDER BY job.updated_at_ms DESC
 LIMIT 1
),
retryable=0,
failed_item_count=(
 SELECT count(*)
 FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id
 AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
skipped_mapping_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='SKIPPED_MAPPING'
),
review_pending_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_PENDING'
),
published_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='PUBLISHED'
),
review_discarded_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_DISCARDED'
),
existing_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='SKIPPED_EXISTING'
),
blocked_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
cancelled_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?`

const recoverTerminalImportsSQLScope = `state IN ('SCANNING','RUNNING') AND EXISTS(
 SELECT 1 FROM jobs job WHERE job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=emulationstation_imports.id
 AND job.state='FAILED'
 AND job.finished_at_ms=?
 AND job.error_code IN ('EMULATIONSTATION_EXECUTION_TIMEOUT','EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED')
)`

const automaticRetryDelaySQL = `CASE attempt_count
 WHEN 1 THEN 1000
 WHEN 2 THEN 5000
 WHEN 3 THEN 30000
 ELSE 120000
END`

func (service *Service) recoverWork(ctx context.Context) error {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/start recovery transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := recoverCancelledExecutions(ctx, transaction, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT job.id,'EMULATIONSTATION_IMPORT',job.scope_id,'FAILED',json_object('schemaVersion',1,'code',
 CASE WHEN job.execution_deadline_at_ms<=?+`+automaticRetryDelaySQL+`
   THEN 'EMULATIONSTATION_EXECUTION_TIMEOUT'
   ELSE 'EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED'
 END),?
FROM jobs job WHERE job.scope_type='EMULATIONSTATION_IMPORT' AND job.state='RUNNING' AND job.leased_until_ms<=?
AND (job.execution_deadline_at_ms<=?+`+automaticRetryDelaySQL+` OR job.attempt_count>=job.max_attempts)
`, now, now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/recover terminal event: %w", err)
	}
	if _, err := recordstore.UpdateEmulationstationImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state='COMMIT_FAILED',error_code=(
 SELECT CASE WHEN job.execution_deadline_at_ms<=?+CASE attempt_count
 WHEN 1 THEN 1000
 WHEN 2 THEN 5000
 WHEN 3 THEN 30000
 ELSE 120000
END
  THEN 'EMULATIONSTATION_EXECUTION_TIMEOUT'
  ELSE 'EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED'
 END FROM jobs job WHERE job.scope_type='EMULATIONSTATION_IMPORT'
 AND job.scope_id=emulationstation_import_items.import_id
 AND job.state='RUNNING'
),
retryable=0,completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
import_id IN (SELECT scope_id FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT' AND state='RUNNING'
 AND leased_until_ms<=? AND (execution_deadline_at_ms<=?+CASE attempt_count
 WHEN 1 THEN 1000
 WHEN 2 THEN 5000
 WHEN 3 THEN 30000
 ELSE 120000
END OR attempt_count>=max_attempts))
AND execution_state IN ('PENDING','COPYING','VALIDATING')
`,
			Args: []any{now, now},
		},
		Values: []any{now, now, now},
	}); err != nil {
		return fmt.Errorf("emulationstationimport/recover terminal item: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='FAILED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=CASE WHEN execution_deadline_at_ms<=?+`+automaticRetryDelaySQL+`
  THEN 'EMULATIONSTATION_EXECUTION_TIMEOUT'
  ELSE 'EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED'
END,
error_retryable=0,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE scope_type='EMULATIONSTATION_IMPORT' AND state='RUNNING' AND leased_until_ms<=?
AND (execution_deadline_at_ms<=?+`+automaticRetryDelaySQL+` OR attempt_count>=max_attempts)`,
		now, now, now, now, now,
	); err != nil {
		return fmt.Errorf("emulationstationimport/recover terminal job: %w", err)
	}
	if _, err := recordstore.UpdateEmulationstationImports(ctx, transaction, recordstore.Update{
		Set: recoverTerminalImportsSQLAssignments,
		Scope: recordstore.Scope{
			Where: recoverTerminalImportsSQLScope,
			Args:  []any{now},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("emulationstationimport/recover terminal aggregate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT job.id,'EMULATIONSTATION_IMPORT',job.scope_id,'RETRY_SCHEDULED',json_object(
 'schemaVersion',1,'executionNo',job.execution_no,'attempt',job.attempt_count,
 'retryAtMs',?+`+automaticRetryDelaySQL+`,'errorCode','EMULATIONSTATION_WORKER_LEASE_EXPIRED',
 'errorRetryable',json('true')
),?
FROM jobs job WHERE job.scope_type='EMULATIONSTATION_IMPORT' AND job.state='RUNNING'
AND job.leased_until_ms<=? AND job.attempt_count<job.max_attempts
AND job.execution_deadline_at_ms>?+`+automaticRetryDelaySQL+`
`, now, now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/recover retry event: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?+`+automaticRetryDelaySQL+`,
leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE scope_type='EMULATIONSTATION_IMPORT' AND state='RUNNING' AND leased_until_ms<=?
AND execution_deadline_at_ms>?+`+automaticRetryDelaySQL+` AND attempt_count<max_attempts`,
		now, now, now, now,
	); err != nil {
		return fmt.Errorf("emulationstationimport/recover job: %w", err)
	}
	if _, err := recordstore.UpdateEmulationstationImports(ctx, transaction, recordstore.Update{
		Set: `
state=CASE WHEN state='RUNNING' THEN 'QUEUED' ELSE state END,
phase=CASE WHEN state='SCANNING' THEN 'DISCOVERING_GAMELISTS' ELSE NULL END,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id IN (
 SELECT scope_id FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT' AND state='QUEUED'
)
AND state IN ('SCANNING','RUNNING')
`,
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("emulationstationimport/recover aggregate: %w", err)
	}
	if err := scheduleAllTerminalItems(ctx, transaction, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit recovery: %w", err)
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
