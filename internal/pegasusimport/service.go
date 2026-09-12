package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	repository "retrom/internal/persistence/pegasusimport"

	application "retrom/internal/service/pegasusimport"

	"retrom/internal/dbexec"

	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/service/tagging"
)

var (
	ErrNotFound         = application.ErrNotFound
	ErrMetadataAbsent   = application.ErrMetadataAbsent
	ErrScanLimit        = application.ErrScanLimit
	ErrMapping          = application.ErrMapping
	ErrVersionConflict  = application.ErrVersionConflict
	ErrNoSelection      = application.ErrNoSelection
	ErrSourceChanged    = application.ErrSourceChanged
	ErrExpired          = application.ErrExpired
	ErrActive           = application.ErrActive
	ErrInvalid          = application.ErrInvalid
	ErrNotCancellable   = application.ErrNotCancellable
	ErrNotRetryable     = application.ErrNotRetryable
	errItemStateChanged = errors.New("item state changed")
)

type Root struct {
	ID, Label string
	path      string
	digest    string
}

type CreateRequest = application.CreateRequest

type (
	RootRef            = application.RootRef
	CreatedBy          = application.CreatedBy
	Counts             = application.Counts
	Summary            = application.Summary
	Collection         = application.Collection
	Mapping            = application.Mapping
	Item               = application.Item
	FailureDetails     = application.FailureDetails
	RuntimeCheck       = application.RuntimeCheck
	RuntimeDependency  = application.RuntimeDependency
	RuntimeBIOS        = application.RuntimeBIOS
	RuntimeMissingDisc = application.RuntimeMissingDisc
	ItemMedia          = application.ItemMedia
	ExistingMatch      = application.ExistingMatch
)

type Service struct {
	sourceReader func(context.Context) (func(), error)
	database     *sql.DB
	blobs        *blobstore.Store
	importer     *libraryimport.Service
	roots        map[string]Root
	now          func() time.Time
	tags         *tagging.Service
	wake         chan struct{}
	stop         chan struct{}
	stopOnce     sync.Once
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
		_ = service.ExpirePlans(context.Background())
		for {
			unit, ok := service.claim(context.Background())
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
 WHERE job.scope_type='PEGASUS_IMPORT'
 AND job.scope_id=pegasus_imports.id
 AND job.state='FAILED'
 ORDER BY job.updated_at_ms DESC
 LIMIT 1
),
retryable=0,
failed_item_count=(
 SELECT count(*)
 FROM pegasus_import_items item
 WHERE item.import_id=pegasus_imports.id
 AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
completed_at_ms=?,version=version+1,updated_at_ms=?`

const recoverTerminalImportsSQLScope = `state IN ('SCANNING','RUNNING') AND EXISTS(
 SELECT 1 FROM jobs job WHERE job.scope_type='PEGASUS_IMPORT' AND job.scope_id=pegasus_imports.id
 AND job.state='FAILED'
 AND job.finished_at_ms=?
 AND job.error_code IN ('PEGASUS_EXECUTION_TIMEOUT','PEGASUS_WORKER_ATTEMPTS_EXHAUSTED')
)`

func (service *Service) recoverWork(ctx context.Context) error {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start recovery transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT job.id,'PEGASUS_IMPORT',job.scope_id,'FAILED',json_object('schemaVersion',1,'code',
 CASE WHEN job.execution_deadline_at_ms<=?
   THEN 'PEGASUS_EXECUTION_TIMEOUT'
   ELSE 'PEGASUS_WORKER_ATTEMPTS_EXHAUSTED'
 END),?
FROM jobs job WHERE job.scope_type='PEGASUS_IMPORT' AND job.state='RUNNING' AND job.leased_until_ms<=?
AND (job.execution_deadline_at_ms<=? OR job.attempt_count>=job.max_attempts)
`, now, now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover terminal event: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state='COMMIT_FAILED',error_code='PEGASUS_WORKER_ATTEMPTS_EXHAUSTED',
retryable=0,completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
import_id IN (SELECT scope_id FROM jobs WHERE scope_type='PEGASUS_IMPORT' AND state='RUNNING'
 AND leased_until_ms<=? AND (execution_deadline_at_ms<=? OR attempt_count>=max_attempts))
AND execution_state IN ('COPYING','VALIDATING')
`,
			Args: []any{now, now},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/recover terminal item: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='FAILED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=CASE WHEN execution_deadline_at_ms<=?
  THEN 'PEGASUS_EXECUTION_TIMEOUT'
  ELSE 'PEGASUS_WORKER_ATTEMPTS_EXHAUSTED'
END,
error_retryable=0,version=version+1,updated_at_ms=?
WHERE scope_type='PEGASUS_IMPORT' AND state='RUNNING' AND leased_until_ms<=?
AND (execution_deadline_at_ms<=? OR attempt_count>=max_attempts)`, now, now, now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover terminal job: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: recoverTerminalImportsSQLAssignments,
		Scope: recordstore.Scope{
			Where: recoverTerminalImportsSQLScope,
			Args:  []any{now},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/recover terminal aggregate: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `execution_state='PENDING',completed_at_ms=NULL,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `
import_id IN (
 SELECT scope_id
 FROM jobs
 WHERE scope_type='PEGASUS_IMPORT'
 AND state='RUNNING'
 AND leased_until_ms<=?
 AND execution_deadline_at_ms>?
)
AND execution_state IN ('COPYING','VALIDATING')
`,
			Args: []any{now, now},
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/recover active item: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE scope_type='PEGASUS_IMPORT' AND state='RUNNING' AND leased_until_ms<=?
AND execution_deadline_at_ms>? AND attempt_count<max_attempts`, now, now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover job: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: `state='QUEUED',phase=NULL,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `
id IN (
 SELECT scope_id FROM jobs WHERE scope_type='PEGASUS_IMPORT' AND state='QUEUED'
)
AND state='RUNNING'
`,
		},
		Values: []any{now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/recover aggregate: %w", err)
	}
	if err := scheduleAllTerminalItems(ctx, transaction, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit recovery: %w", err)
	}
	return nil
}

type work struct {
	JobID, ImportID, Kind, RootID, RootDigest, RelativePath string
	CreatedByUserID                                         string
	ExecutionNo, Attempt, DeadlineAtMS                      int64
}

func (service *Service) claim(ctx context.Context) (work, bool) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return work{}, false
	}
	defer dbexec.Rollback(transaction)
	now := service.now().UnixMilli()
	var unit work
	if err := transaction.QueryRowContext(ctx, `
SELECT job.id,import.id,job.kind,import.root_id,import.root_config_digest,import.source_relative_path,
import.created_by_user_id,
job.execution_no,job.attempt_count
FROM jobs job JOIN pegasus_imports import ON import.id=job.scope_id
WHERE job.scope_type='PEGASUS_IMPORT' AND job.kind IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT')
AND job.state='QUEUED' AND job.available_at_ms<=?
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1
`, now).Scan(&unit.JobID, &unit.ImportID, &unit.Kind, &unit.RootID, &unit.RootDigest,
		&unit.RelativePath, &unit.CreatedByUserID, &unit.ExecutionNo, &unit.Attempt); err != nil {
		return work{}, false
	}
	duration := int64((30 * time.Minute) / time.Millisecond)
	if unit.Kind == "SERVER_PEGASUS_IMPORT" {
		duration = int64((8 * time.Hour) / time.Millisecond)
	}
	unit.Attempt++
	unit.DeadlineAtMS = now + duration
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',attempt_count=attempt_count+1,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),
leased_until_ms=?,heartbeat_at_ms=?,worker_id='pegasus-import-worker',
version=version+1,updated_at_ms=? WHERE id=? AND state='QUEUED'
`, now, unit.DeadlineAtMS, now+60000, now, now, unit.JobID)
	if err != nil {
		return work{}, false
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return work{}, false
	}
	phase := "DISCOVERING_METADATA"
	if unit.Kind == "SERVER_PEGASUS_IMPORT" {
		phase = "COPYING_CONTENT"
	}
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: `
state=CASE WHEN ?='SERVER_PEGASUS_IMPORT' THEN 'RUNNING' ELSE state END,
phase=?,started_at_ms=CASE WHEN ?='SERVER_PEGASUS_IMPORT' THEN COALESCE(started_at_ms,?) ELSE
started_at_ms END,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{unit.Kind, phase, unit.Kind, now, now},
	}); err != nil {
		return work{}, false
	}
	event, _ := json.Marshal(
		map[string]any{"schemaVersion": 1, "executionNo": unit.ExecutionNo, "attempt": unit.Attempt},
	)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'STARTED',?,?)
`, unit.JobID, unit.ImportID, string(event), now); err != nil {
		return work{}, false
	}
	if err := transaction.Commit(); err != nil {
		return work{}, false
	}
	return unit, true
}

func (service *Service) execute(ctx context.Context, unit work) {
	if unit.DeadlineAtMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(unit.DeadlineAtMS))
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
	if unit.Kind == "SERVER_PEGASUS_SCAN" {
		service.executeScan(ctx, unit, root)
		return
	}
	service.executeImport(ctx, unit, root)
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
			now := service.now().UnixMilli()
			_, _ = service.database.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id='pegasus-import-worker'
`, now, now+60000, now, unit.JobID)
		}
	}
}

func (service *Service) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	creation := application.NewCreation(
		repository.NewCreation(service.database), creationSourceSelector{service}, service.now,
	)
	result, err := creation.Create(

		ctx,

		request,

		actorID,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("create Pegasus import: %w", err)
	}
	service.signal()
	return result, nil
}

type creationSourceSelector struct{ service *Service }

func (source creationSourceSelector) Select(
	ctx context.Context,
	rootID, path string,
) (application.SelectedRoot, error) {
	if err := ctx.Err(); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("select Pegasus source: %w", err)
	}
	root, err := source.service.validateCreateRequest(CreateRequest{RootID: rootID, SourceRelativePath: path})
	if err != nil {
		return application.SelectedRoot{}, err
	}
	return application.SelectedRoot{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}

func (service *Service) validateCreateRequest(request CreateRequest) (Root, error) {
	if err := serversource.ValidateRootID(request.RootID); err != nil {
		return Root{}, fmt.Errorf("pegasusimport/validate root ID: %w", err)
	}
	root, ok := service.roots[request.RootID]
	if !ok {
		return Root{}, serversource.ErrRootNotFound
	}
	if err := serversource.ValidateRelativePath(request.SourceRelativePath); err != nil {
		return Root{}, fmt.Errorf("pegasusimport/validate source path: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(root.path, request.SourceRelativePath)
	if err != nil {
		return Root{}, serversource.ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return root, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stableStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func errorCode(err error) string {
	candidates := []error{
		ErrMetadataAbsent,
		ErrScanLimit,
		ErrSourceChanged,
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
	if err != nil && strings.HasPrefix(err.Error(), "PEGASUS_") {
		return strings.SplitN(err.Error(), ":", 2)[0]
	}
	return "INTERNAL_ERROR"
}
