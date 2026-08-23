package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/tagging"
)

var (
	ErrNotFound             = errors.New("EMULATIONSTATION_IMPORT_NOT_FOUND")
	ErrGamelistAbsent       = errors.New("EMULATIONSTATION_GAMELIST_NOT_FOUND")
	ErrNoValidGamelist      = errors.New("EMULATIONSTATION_NO_VALID_GAMELIST")
	ErrScanLimit            = errors.New("EMULATIONSTATION_SCAN_LIMIT_EXCEEDED")
	ErrMapping              = errors.New("EMULATIONSTATION_MAPPING_INCOMPLETE")
	ErrVersionConflict      = errors.New("VERSION_CONFLICT")
	ErrNoSelection          = errors.New("EMULATIONSTATION_NO_COLLECTION_SELECTED")
	ErrSourceChanged        = errors.New("EMULATIONSTATION_SOURCE_CHANGED")
	ErrMappingTargetChanged = errors.New("EMULATIONSTATION_MAPPING_TARGET_CHANGED")
	ErrExpired              = errors.New("EMULATIONSTATION_PLAN_EXPIRED")
	ErrActive               = errors.New("EMULATIONSTATION_IMPORT_ACTIVE")
	ErrInvalid              = errors.New("EMULATIONSTATION_IMPORT_INVALID")
	ErrNotCancellable       = errors.New("EMULATIONSTATION_IMPORT_NOT_CANCELLABLE")
	ErrNotRetryable         = errors.New("EMULATIONSTATION_IMPORT_NOT_RETRYABLE")
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
	configured []config.ServerImportRoot,
	now func() time.Time,
) *Service {
	roots := make(map[string]Root, len(configured))
	for _, configuredRoot := range configured {
		digest := credentials.ServerImportRootDigest(configuredRoot.ID, configuredRoot.CanonicalPath)
		roots[configuredRoot.ID] = Root{
			ID:     configuredRoot.ID,
			Label:  configuredRoot.Label,
			path:   configuredRoot.CanonicalPath,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	return &Service{
		database: database, blobs: blobs, importer: importer, roots: roots, now: now, tags: tagging.New(database, now),
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
			unit, ok := service.claim(context.Background())
			if !ok {
				break
			}
			service.execute(context.Background(), unit)
		}
	}
}

const recoverTerminalImportsSQL = `
UPDATE emulationstation_imports SET state='FAILED',phase=NULL,
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
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('SCANNING','RUNNING') AND EXISTS(
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
	defer cleanup.Rollback(transaction)
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='COMMIT_FAILED',error_code=(
 SELECT CASE WHEN job.execution_deadline_at_ms<=?+`+automaticRetryDelaySQL+`
  THEN 'EMULATIONSTATION_EXECUTION_TIMEOUT'
  ELSE 'EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED'
 END FROM jobs job WHERE job.scope_type='EMULATIONSTATION_IMPORT'
 AND job.scope_id=emulationstation_import_items.import_id
 AND job.state='RUNNING'
),
retryable=0,completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_id IN (SELECT scope_id FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT' AND state='RUNNING'
 AND leased_until_ms<=? AND (execution_deadline_at_ms<=?+`+automaticRetryDelaySQL+` OR attempt_count>=max_attempts))
AND execution_state IN ('PENDING','COPYING','VALIDATING')`, now, now, now, now, now); err != nil {
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
	if _, err := transaction.ExecContext(ctx, recoverTerminalImportsSQL, now, now, now); err != nil {
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_imports SET
state=CASE WHEN state='RUNNING' THEN 'QUEUED' ELSE state END,
phase=CASE WHEN state='SCANNING' THEN 'DISCOVERING_GAMELISTS' ELSE NULL END,
version=version+1,updated_at_ms=?
WHERE id IN (
 SELECT scope_id FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT' AND state='QUEUED'
)
AND state IN ('SCANNING','RUNNING')`, now); err != nil {
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

type work struct {
	JobID, ImportID, Kind, RootID, RootDigest, RelativePath string
	CreatedByUserID                                         string
	ExecutionNo, Attempt, DeadlineAtMS                      int64
	ReleaseYearMax                                          int
}

func (service *Service) claim(ctx context.Context) (work, bool) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return work{}, false
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	var unit work
	var frozenDeadline sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT job.id,import.id,job.kind,import.root_id,import.root_config_digest,import.source_relative_path,
import.created_by_user_id,import.release_year_max,
job.execution_no,job.attempt_count,job.execution_deadline_at_ms
FROM jobs job JOIN emulationstation_imports import ON import.id=job.scope_id
WHERE job.scope_type='EMULATIONSTATION_IMPORT'
AND job.kind IN ('SERVER_EMULATIONSTATION_SCAN','SERVER_EMULATIONSTATION_IMPORT')
AND job.state='QUEUED' AND job.available_at_ms<=? AND job.attempt_count<job.max_attempts
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1
`, now).Scan(&unit.JobID, &unit.ImportID, &unit.Kind, &unit.RootID, &unit.RootDigest,
		&unit.RelativePath, &unit.CreatedByUserID, &unit.ReleaseYearMax,
		&unit.ExecutionNo, &unit.Attempt, &frozenDeadline); err != nil {
		return work{}, false
	}
	duration := int64((8 * time.Hour) / time.Millisecond)
	unit.Attempt++
	unit.DeadlineAtMS = now + duration
	if frozenDeadline.Valid {
		unit.DeadlineAtMS = frozenDeadline.Int64
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',attempt_count=attempt_count+1,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),
leased_until_ms=?,heartbeat_at_ms=?,worker_id='emulationstation-import-worker',
version=version+1,updated_at_ms=? WHERE id=? AND state='QUEUED' AND attempt_count<max_attempts
`, now, unit.DeadlineAtMS, now+60000, now, now, unit.JobID)
	if err != nil {
		return work{}, false
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return work{}, false
	}
	phase := "DISCOVERING_GAMELISTS"
	if unit.Kind == "SERVER_EMULATIONSTATION_IMPORT" {
		phase = "COPYING_CONTENT"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_imports
SET state=CASE WHEN ?='SERVER_EMULATIONSTATION_IMPORT' THEN 'RUNNING' ELSE state END,
phase=?,
started_at_ms=CASE
 WHEN ?='SERVER_EMULATIONSTATION_IMPORT' THEN COALESCE(started_at_ms,?)
 ELSE started_at_ms
END,
version=version+1,updated_at_ms=? WHERE id=?
`, unit.Kind, phase, unit.Kind, now, now, unit.ImportID); err != nil {
		return work{}, false
	}
	event, _ := json.Marshal(
		map[string]any{"schemaVersion": 1, "executionNo": unit.ExecutionNo, "attempt": unit.Attempt},
	)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'STARTED',?,?)
`, unit.JobID, unit.ImportID, string(event), now); err != nil {
		return work{}, false
	}
	if err := transaction.Commit(); err != nil {
		return work{}, false
	}
	return unit, true
}

func (service *Service) execute(ctx context.Context, unit work) {
	if unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= service.now().UnixMilli() {
		service.fail(ctx, unit, "EMULATIONSTATION_EXECUTION_TIMEOUT", false)
		return
	}
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
			now := service.now().UnixMilli()
			_, _ = service.database.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id='emulationstation-import-worker'
`, now, now+60000, now, unit.JobID)
		}
	}
}

func (service *Service) Create(ctx context.Context, request CreateRequest, userID string) (Summary, error) {
	root, err := service.validateCreateRequest(request)
	if err != nil {
		return Summary{}, err
	}
	if err := service.ensurePlanCapacity(ctx); err != nil {
		return Summary{}, err
	}
	return service.createScanPlan(ctx, request, userID, root)
}

func (service *Service) validateCreateRequest(request CreateRequest) (Root, error) {
	if err := serversource.ValidateRootID(request.RootID); err != nil {
		return Root{}, fmt.Errorf("emulationstationimport/validate root ID: %w", err)
	}
	root, ok := service.roots[request.RootID]
	if !ok {
		return Root{}, serversource.ErrRootNotFound
	}
	if err := serversource.ValidateRelativePath(request.SourceRelativePath); err != nil {
		return Root{}, fmt.Errorf("emulationstationimport/validate source path: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(root.path, request.SourceRelativePath)
	if err != nil {
		return Root{}, serversource.ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return root, nil
}

func (service *Service) ensurePlanCapacity(ctx context.Context) error {
	var plans int
	if err := service.database.QueryRowContext(ctx, `
SELECT count(*) FROM emulationstation_imports WHERE state IN ('SCANNING','AWAITING_MAPPING')
`).Scan(&plans); err != nil {
		return fmt.Errorf("emulationstationimport/count plans: %w", err)
	}
	if plans >= 20 {
		return ErrActive
	}
	return nil
}

func (service *Service) createScanPlan(
	ctx context.Context,
	request CreateRequest,
	userID string,
	root Root,
) (Summary, error) {
	importID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	releaseYearMax := service.now().UTC().Year() + 1
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_EMULATIONSTATION_SCAN",
		"scope":       map[string]any{"type": "EMULATIONSTATION_IMPORT", "id": importID.String()},
		"executionId": executionID.String(),
		"inputs": map[string]any{
			"rootId":             root.ID,
			"sourceRelativePath": request.SourceRelativePath,
			"rootConfigDigest":   root.digest,
			"releaseYearMax":     releaseYearMax,
		},
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	dedupe := jobDedupe("SERVER_EMULATIONSTATION_SCAN", importID.String())
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/create transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'SERVER_EMULATIONSTATION_SCAN',?,1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?)
`, jobID.String(), importID.String(), dedupe, now, now, now); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/create scan job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/create input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO emulationstation_imports(
	id,root_id,root_label_snapshot,source_relative_path,root_config_digest,release_year_max,state,phase,
	scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
	VALUES(?,?,?,?,?,?,'SCANNING','DISCOVERING_GAMELISTS',?,?,?,?,?)
	`,
		importID.String(), root.ID, root.Label, request.SourceRelativePath, root.digest,
		releaseYearMax, jobID.String(), userID, now, now,
		now+int64((7*24*time.Hour)/time.Millisecond)); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/create plan: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)
`, jobID.String(), importID.String(), now); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/create event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(
 ?,'USER',?,NULL,'EMULATIONSTATION_IMPORT_CREATED','EMULATIONSTATION_IMPORT',?,
 NULL,'{"state":"SCANNING"}',NULL,NULL,?
)
`, auditID.String(), userID, importID.String(), now); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/create audit: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/commit create: %w", err)
	}
	service.signal()
	return service.Get(ctx, importID.String())
}

func jobDedupe(kind, value string) string {
	digest := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00" + kind + "\x00" + value))
	return hex.EncodeToString(digest[:])
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func jsonStrings(value string) []string {
	result := []string{}
	_ = json.Unmarshal([]byte(value), &result)
	return result
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
