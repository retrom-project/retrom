package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
)

var (
	ErrNotFound       = errors.New("PEGASUS_IMPORT_NOT_FOUND")
	ErrMetadataAbsent = errors.New("PEGASUS_METADATA_NOT_FOUND")
	ErrScanLimit      = errors.New("PEGASUS_SCAN_LIMIT_EXCEEDED")
	ErrMapping        = errors.New("PEGASUS_MAPPING_INCOMPLETE")
	ErrNoSelection    = errors.New("PEGASUS_NO_COLLECTION_SELECTED")
	ErrSourceChanged  = errors.New("PEGASUS_SOURCE_CHANGED")
	ErrExpired        = errors.New("PEGASUS_PLAN_EXPIRED")
	ErrActive         = errors.New("PEGASUS_IMPORT_ACTIVE")
	ErrInvalid        = errors.New("PEGASUS_IMPORT_INVALID")
	ErrNotCancellable = errors.New("PEGASUS_IMPORT_NOT_CANCELLABLE")
	ErrNotRetryable   = errors.New("PEGASUS_IMPORT_NOT_RETRYABLE")
)

type Root struct {
	ID, Label string
	path      string
	digest    string
}

type CreateRequest struct {
	RootID             string `json:"rootId"`
	SourceRelativePath string `json:"sourceRelativePath"`
}

type RootRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type CreatedBy struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Counts struct {
	Metadata             int64 `json:"metadata"`
	InvalidMetadata      int64 `json:"invalidMetadata"`
	Collections          int64 `json:"collections"`
	Games                int64 `json:"games"`
	EstimatedSourceBytes int64 `json:"estimatedSourceBytes"`
	MappedCollections    int64 `json:"mappedCollections"`
	SkippedCollections   int64 `json:"skippedCollections"`
	Processable          int64 `json:"processable"`
	Blocked              int64 `json:"blocked"`
	Published            int64 `json:"published"`
	Existing             int64 `json:"existing"`
	Failed               int64 `json:"failed"`
	Cancelled            int64 `json:"cancelled"`
	MediaWarnings        int64 `json:"mediaWarnings"`
	Covers               int64 `json:"covers"`
	Videos               int64 `json:"videos"`
}

type Summary struct {
	ID                 string    `json:"id"`
	Root               RootRef   `json:"root"`
	SourceRelativePath string    `json:"sourceRelativePath"`
	State              string    `json:"state"`
	Phase              *string   `json:"phase"`
	ScanJobID          string    `json:"scanJobId"`
	ImportJobID        *string   `json:"importJobId"`
	Counts             Counts    `json:"counts"`
	MappingVersion     int64     `json:"mappingVersion"`
	Version            int64     `json:"version"`
	CreatedBy          CreatedBy `json:"createdBy"`
	LastErrorCode      *string   `json:"lastErrorCode"`
	Retryable          bool      `json:"retryable"`
	CreatedAtMS        int64     `json:"createdAtMs"`
	UpdatedAtMS        int64     `json:"updatedAtMs"`
	ExpiresAtMS        int64     `json:"expiresAtMs"`
	CompletedAtMS      *int64    `json:"completedAtMs"`
}

type Collection struct {
	ID                         string   `json:"id"`
	MetadataRelativePath       string   `json:"metadataRelativePath"`
	SegmentOrdinal             int64    `json:"segmentOrdinal"`
	Name                       string   `json:"name"`
	ShortName                  *string  `json:"shortName"`
	Description                string   `json:"description"`
	GameCount                  int64    `json:"gameCount"`
	IssueCount                 int64    `json:"issueCount"`
	MappingAction              *string  `json:"mappingAction"`
	TargetPlatformInstanceID   *string  `json:"targetPlatformInstanceId"`
	TargetPlatformInstanceName *string  `json:"targetPlatformInstanceName"`
	TargetDefaultCoreID        *string  `json:"targetDefaultCoreId"`
	TargetDefaultCoreName      *string  `json:"targetDefaultCoreName"`
	IgnoredRules               []string `json:"ignoredRules"`
	WarningFields              []string `json:"warningFields"`
}

type Mapping struct {
	CollectionID       string `json:"collectionId"`
	Action             string `json:"action"`
	PlatformInstanceID string `json:"platformInstanceId,omitempty"`
}

type Item struct {
	ID                         string           `json:"id"`
	Title                      string           `json:"title"`
	CollectionID               *string          `json:"collectionId"`
	CollectionName             *string          `json:"collectionName"`
	TargetPlatformInstanceID   *string          `json:"targetPlatformInstanceId"`
	TargetPlatformInstanceName *string          `json:"targetPlatformInstanceName"`
	MetadataRelativePath       string           `json:"metadataRelativePath"`
	ExecutionState             string           `json:"executionState"`
	ContentKind                *string          `json:"contentKind"`
	Media                      ItemMedia        `json:"media"`
	Warnings                   []map[string]any `json:"warnings"`
	DiscoveryCode              *string          `json:"discoveryCode"`
	ErrorCode                  *string          `json:"errorCode"`
	FailureDetails             *FailureDetails  `json:"failureDetails"`
	RuntimeCheck               *RuntimeCheck    `json:"runtimeCheck"`
	Retryable                  bool             `json:"retryable"`
	PublishedGameID            *string          `json:"publishedGameId"`
	ExistingGameID             *string          `json:"existingGameId"`
	ExistingMatches            []ExistingMatch  `json:"existingMatches"`
	UpdatedAtMS                int64            `json:"updatedAtMs"`
}

type FailureDetails struct {
	SchemaVersion       int64   `json:"schemaVersion"`
	Stage               string  `json:"stage"`
	Operation           string  `json:"operation"`
	CauseCode           string  `json:"causeCode"`
	TechnicalDetail     string  `json:"technicalDetail"`
	RelativePath        *string `json:"relativePath"`
	ObservedFileCount   *int64  `json:"observedFileCount"`
	AllowedFileCount    *int64  `json:"allowedFileCount"`
	LibraryImportJobID  *string `json:"libraryImportJobId"`
	LibraryImportItemID *string `json:"libraryImportItemId"`
}

type RuntimeCheck struct {
	Status            string               `json:"status"`
	Code              string               `json:"code"`
	CoreID            string               `json:"coreId"`
	CoreName          string               `json:"coreName"`
	Machine           *string              `json:"machine"`
	MissingEntries    []string             `json:"missingEntries"`
	MismatchedEntries []string             `json:"mismatchedEntries"`
	Dependencies      []RuntimeDependency  `json:"dependencies"`
	BIOS              []RuntimeBIOS        `json:"bios"`
	MissingDiscs      []RuntimeMissingDisc `json:"missingDiscs"`
}

type RuntimeDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy"`
	ExpectedLogicalName string   `json:"expectedLogicalName"`
	State               string   `json:"state"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type RuntimeBIOS struct {
	LogicalName        string  `json:"logicalName"`
	RequirementMode    string  `json:"requirementMode"`
	ConditionCode      *string `json:"conditionCode"`
	InstallationStatus *string `json:"installationStatus"`
}

type RuntimeMissingDisc struct {
	Ordinal         int64  `json:"ordinal"`
	SourceReference string `json:"sourceReference"`
}

type ItemMedia struct {
	Cover string `json:"cover"`
	Video string `json:"video"`
}

type ExistingMatch struct {
	GameID            string `json:"gameId"`
	ContentRevisionID string `json:"contentRevisionId"`
}

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	importer *libraryimport.Service
	roots    map[string]Root
	now      func() time.Time
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
		database: database, blobs: blobs, importer: importer, roots: roots, now: now,
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

const recoverPublishedItemsSQL = `
UPDATE pegasus_import_items SET execution_state='PUBLISHED',published_game_id=(
 SELECT game.id FROM games game JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
 JOIN game_content_revisions content ON content.id=game.current_content_revision_id
 WHERE metadata.source_kind='SERVER_PEGASUS_IMPORT' AND metadata.source_ref_id=pegasus_import_items.id
 AND content.source_kind='SERVER_PEGASUS_IMPORT' AND content.source_ref_id=pegasus_import_items.id LIMIT 1
),completed_at_ms=?,updated_at_ms=?
WHERE execution_state='PUBLISHING' AND EXISTS(
 SELECT 1 FROM games game JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
 JOIN game_content_revisions content ON content.id=game.current_content_revision_id
 WHERE metadata.source_kind='SERVER_PEGASUS_IMPORT' AND metadata.source_ref_id=pegasus_import_items.id
 AND content.source_kind='SERVER_PEGASUS_IMPORT' AND content.source_ref_id=pegasus_import_items.id
)`

const recoverTerminalImportsSQL = `
UPDATE pegasus_imports SET state='FAILED',phase=NULL,
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
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('SCANNING','RUNNING') AND EXISTS(
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
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, recoverPublishedItemsSQL, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover published item: %w", err)
	}
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items SET execution_state='COMMIT_FAILED',error_code='PEGASUS_WORKER_ATTEMPTS_EXHAUSTED',
retryable=0,completed_at_ms=?,updated_at_ms=?
WHERE import_id IN (SELECT scope_id FROM jobs WHERE scope_type='PEGASUS_IMPORT' AND state='RUNNING'
 AND leased_until_ms<=? AND (execution_deadline_at_ms<=? OR attempt_count>=max_attempts))
AND execution_state IN ('COPYING','VALIDATING','PUBLISHING')`, now, now, now, now); err != nil {
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
	if _, err := transaction.ExecContext(ctx, recoverTerminalImportsSQL, now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover terminal aggregate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items SET execution_state='PENDING',completed_at_ms=NULL,updated_at_ms=?
WHERE import_id IN (
 SELECT scope_id
 FROM jobs
 WHERE scope_type='PEGASUS_IMPORT'
 AND state='RUNNING'
 AND leased_until_ms<=?
 AND execution_deadline_at_ms>?
)
AND execution_state IN ('COPYING','VALIDATING','PUBLISHING')`, now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover active item: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE scope_type='PEGASUS_IMPORT' AND state='RUNNING' AND leased_until_ms<=?
AND execution_deadline_at_ms>? AND attempt_count<max_attempts`, now, now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/recover job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports SET state='QUEUED',phase=NULL,version=version+1,updated_at_ms=?
WHERE id IN (
 SELECT scope_id FROM jobs WHERE scope_type='PEGASUS_IMPORT' AND state='QUEUED'
)
AND state='RUNNING'`, now); err != nil {
		return fmt.Errorf("pegasusimport/recover aggregate: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit recovery: %w", err)
	}
	return nil
}

type work struct {
	JobID, ImportID, Kind, RootID, RootDigest, RelativePath string
	ExecutionNo, Attempt, DeadlineAtMS                      int64
}

func (service *Service) claim(ctx context.Context) (work, bool) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return work{}, false
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	var unit work
	if err := transaction.QueryRowContext(ctx, `
SELECT job.id,import.id,job.kind,import.root_id,import.root_config_digest,import.source_relative_path,
job.execution_no,job.attempt_count
FROM jobs job JOIN pegasus_imports import ON import.id=job.scope_id
WHERE job.scope_type='PEGASUS_IMPORT' AND job.kind IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT')
AND job.state='QUEUED' AND job.available_at_ms<=?
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1
`, now).Scan(&unit.JobID, &unit.ImportID, &unit.Kind, &unit.RootID, &unit.RootDigest,
		&unit.RelativePath, &unit.ExecutionNo, &unit.Attempt); err != nil {
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports SET state=CASE WHEN ?='SERVER_PEGASUS_IMPORT' THEN 'RUNNING' ELSE state END,
phase=?,started_at_ms=CASE WHEN ?='SERVER_PEGASUS_IMPORT' THEN COALESCE(started_at_ms,?) ELSE started_at_ms END,
version=version+1,updated_at_ms=? WHERE id=?
`, unit.Kind, phase, unit.Kind, now, now, unit.ImportID); err != nil {
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

func (service *Service) ensurePlanCapacity(ctx context.Context) error {
	var plans int
	if err := service.database.QueryRowContext(ctx, `
SELECT count(*) FROM pegasus_imports WHERE state IN ('SCANNING','AWAITING_MAPPING')
`).Scan(&plans); err != nil {
		return fmt.Errorf("pegasusimport/count plans: %w", err)
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
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_PEGASUS_SCAN",
		"scope":       map[string]any{"type": "PEGASUS_IMPORT", "id": importID.String()},
		"executionId": executionID.String(),
		"inputs": map[string]any{
			"rootId":             root.ID,
			"sourceRelativePath": request.SourceRelativePath,
			"rootConfigDigest":   root.digest,
		},
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	dedupe := jobDedupe("SERVER_PEGASUS_SCAN", importID.String())
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?)
`, jobID.String(), importID.String(), dedupe, now, now, now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create scan job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,
scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,?,?,?,?,'SCANNING','DISCOVERING_METADATA',?,?,?,?,?)
`, importID.String(), root.ID, root.Label, request.SourceRelativePath, root.digest, jobID.String(), userID, now, now,
		now+int64((7*24*time.Hour)/time.Millisecond)); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create plan: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)
`, jobID.String(), importID.String(), now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'PEGASUS_IMPORT_CREATED','PEGASUS_IMPORT',?,NULL,'{"state":"SCANNING"}',NULL,NULL,?)
`, auditID.String(), userID, importID.String(), now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create audit: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/commit create: %w", err)
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
