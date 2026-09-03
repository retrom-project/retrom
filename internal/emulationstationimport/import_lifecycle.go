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
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/serversource"
)

type gamelistEvidence struct {
	path, facts string
	digest      sql.NullString
	size        int64
}

func (service *Service) verifySnapshot(ctx context.Context, importID, selectedPath string, root Root) error {
	values, err := service.loadGamelistEvidence(ctx, importID)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return ErrSourceChanged
	}
	for _, value := range values {
		if err := verifyGamelistEvidence(ctx, root, selectedPath, value); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) loadGamelistEvidence(
	ctx context.Context,
	importID string,
) ([]gamelistEvidence, error) {
	rows, err := service.database.QueryContext(
		ctx,
		`SELECT relative_path,size_bytes,content_digest,source_facts_digest
FROM emulationstation_import_gamelists
WHERE import_id=?
ORDER BY relative_path`,
		importID,
	)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/read metadata evidence: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	values := make([]gamelistEvidence, 0)
	for rows.Next() {
		var value gamelistEvidence
		if err := rows.Scan(&value.path, &value.size, &value.digest, &value.facts); err != nil {
			return nil, ErrSourceChanged
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrSourceChanged
	}
	return values, nil
}

func verifyGamelistEvidence(
	ctx context.Context,
	root Root,
	selectedPath string,
	value gamelistEvidence,
) error {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return fmt.Errorf("emulationstationimport/acquire snapshot reader: %w", err)
	}
	defer release()
	file, before, err := serversource.OpenRelativeFile(root.path, selectedPath, value.path)
	if err != nil || before.Size() != value.size || serversource.FactsDigest(before) != value.facts {
		if file != nil {
			cleanup.Error("close", file.Close())
		}
		return ErrSourceChanged
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	if !value.digest.Valid {
		return nil
	}
	hash := sha256.New()
	if _, err := file.WriteTo(hash); err != nil || ctx.Err() != nil {
		return ErrSourceChanged
	}
	after, err := file.Stat()
	if err != nil || !serversource.SameFileFacts(before, after) ||
		hex.EncodeToString(hash.Sum(nil)) != value.digest.String {
		return ErrSourceChanged
	}
	return nil
}

func (service *Service) StartImport(ctx context.Context, importID string, expectedVersion int64) (Summary, error) {
	summary, err := service.Get(ctx, importID)
	if err != nil {
		return Summary{}, err
	}
	alreadyStarted, err := validateStartSummary(summary, expectedVersion, service.now().UnixMilli())
	if err != nil {
		return Summary{}, err
	}
	if alreadyStarted {
		return summary, nil
	}
	root, err := service.resolveStartRoot(ctx, importID, summary.Root.ID)
	if err != nil {
		return Summary{}, err
	}
	if err := service.verifySnapshot(ctx, importID, summary.SourceRelativePath, root); err != nil {
		return Summary{}, err
	}
	if err := service.verifyMappingTargets(ctx, importID); err != nil {
		return Summary{}, err
	}
	if err := service.queueImport(ctx, summary, root, expectedVersion); err != nil {
		return Summary{}, err
	}
	service.signal()
	return service.Get(ctx, importID)
}

func validateStartSummary(summary Summary, expectedVersion, now int64) (bool, error) {
	if summary.Version != expectedVersion {
		return false, ErrVersionConflict
	}
	switch summary.State {
	case "QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE":
		return true, nil
	case "AWAITING_MAPPING":
	default:
		return false, ErrExpired
	}
	if now >= summary.ExpiresAtMS {
		return false, ErrExpired
	}
	if summary.Counts.MappedCollections+summary.Counts.SkippedCollections != summary.Counts.Collections {
		return false, ErrMapping
	}
	if summary.Counts.MappedCollections == 0 {
		return false, ErrNoSelection
	}
	return false, nil
}

func (service *Service) resolveStartRoot(ctx context.Context, importID, rootID string) (Root, error) {
	root, ok := service.roots[rootID]
	if !ok {
		return Root{}, ErrSourceChanged
	}
	var storedRootDigest string
	err := service.database.QueryRowContext(
		ctx,
		`SELECT root_config_digest FROM emulationstation_imports WHERE id=?`,
		importID,
	).Scan(&storedRootDigest)
	if err != nil || storedRootDigest != root.digest {
		return Root{}, ErrSourceChanged
	}
	return root, nil
}

func (service *Service) verifyMappingTargets(ctx context.Context, importID string) error {
	var changed int
	err := service.database.QueryRowContext(ctx, `
SELECT count(*)
FROM emulationstation_import_collections collection
LEFT JOIN platform_instances instance ON instance.id=collection.target_platform_instance_id
LEFT JOIN runtime_targets target
  ON target.provider_id=collection.target_provider_id AND target.target_id=collection.target_id
LEFT JOIN runtime_target_bindings binding
  ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
LEFT JOIN dat_versions dat ON dat.id=collection.target_dat_version_id
WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND (
 instance.id IS NULL OR instance.enabled<>1 OR instance.deleted_at_ms IS NOT NULL OR
 instance.version<>collection.target_platform_instance_version OR
 instance.platform_id<>collection.target_platform_id OR
 instance.default_core_id<>collection.target_default_core_id OR
 target.provider_id IS NULL OR target.target_contract_sha256<>collection.target_contract_sha256 OR
 binding.core_id<>collection.target_default_core_id OR binding.launch_policy='DISABLED' OR
 collection.target_dat_version_id IS NOT NULL AND (dat.id IS NULL OR dat.is_active<>1)
)`, importID).Scan(&changed)
	if err != nil {
		return fmt.Errorf("emulationstationimport/verify mapping targets: %w", err)
	}
	if changed != 0 {
		return ErrMappingTargetChanged
	}
	return nil
}

func (service *Service) queueImport(
	ctx context.Context,
	summary Summary,
	root Root,
	expectedVersion int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/start transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version FROM emulationstation_imports WHERE id=?`, summary.ID,
	).Scan(&state, &version); err != nil || state != "AWAITING_MAPPING" {
		return ErrMapping
	}
	if version != expectedVersion {
		return ErrVersionConflict
	}
	if err := ensureNoActiveExecution(ctx, transaction, summary.ID); err != nil {
		return err
	}
	if err := validateQueuedMappingTags(ctx, transaction, summary.ID); err != nil {
		return err
	}
	jobID, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	input := queuedImportInput(summary, root, expectedVersion, executionID.String())
	encoded, _ := json.Marshal(input)
	if err := createQueuedImportJob(ctx, transaction, summary.ID, jobID.String(), encoded, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='SKIPPED_MAPPING',completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_id=?
AND collection_id IN (
  SELECT id FROM emulationstation_import_collections WHERE import_id=? AND mapping_action='SKIP'
)`, now, now, summary.ID, summary.ID); err != nil {
		return fmt.Errorf("emulationstationimport/skip mapped items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state=CASE discovery_state
  WHEN 'BLOCKED_SOURCE' THEN 'BLOCKED_SOURCE'
  ELSE 'BLOCKED_CONTENT'
END,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_id=?
AND execution_state='PENDING'
AND discovery_state!='READY'`, now, now, summary.ID); err != nil {
		return fmt.Errorf("emulationstationimport/close discovery items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_imports
SET import_job_id=?,state='QUEUED',phase=NULL,
	skipped_mapping_item_count=(
	  SELECT count(*) FROM emulationstation_import_items
	  WHERE import_id=? AND execution_state='SKIPPED_MAPPING'
	),
blocked_item_count=(
  SELECT count(*)
  FROM emulationstation_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
version=version+1,updated_at_ms=?
WHERE id=?`, jobID.String(), summary.ID, summary.ID, now, summary.ID); err != nil {
		return classifyQueueImportError(ctx, transaction, summary.ID, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(
?,'EMULATIONSTATION_IMPORT',?,'QUEUED',
'{"schemaVersion":1,"executionNo":1,"attempt":0}',?
)`, jobID.String(), summary.ID, now); err != nil {
		return fmt.Errorf("emulationstationimport/queue event: %w", err)
	}
	if err := scheduleTerminalItems(ctx, transaction, summary.ID, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit start: %w", err)
	}
	return nil
}

func ensureNoActiveExecution(ctx context.Context, transaction *sql.Tx, importID string) error {
	var active int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM emulationstation_imports
WHERE id<>? AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, importID).Scan(&active); err != nil {
		return fmt.Errorf("emulationstationimport/check active execution: %w", err)
	}
	if active != 0 {
		return ErrActive
	}
	return nil
}

func classifyQueueImportError(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	queueErr error,
) error {
	if err := ensureNoActiveExecution(ctx, transaction, importID); errors.Is(err, ErrActive) {
		return ErrActive
	}
	return fmt.Errorf("emulationstationimport/queue import: %w", queueErr)
}

func validateQueuedMappingTags(ctx context.Context, transaction *sql.Tx, importID string) error {
	var invalidTagSnapshots int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM emulationstation_import_collections collection
JOIN json_each(collection.tag_snapshot_json) entry
LEFT JOIN tags tag ON tag.id=json_extract(entry.value,'$.tagId') AND tag.status='ACTIVE'
WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND tag.id IS NULL
`, importID).Scan(&invalidTagSnapshots); err != nil {
		return fmt.Errorf("emulationstationimport/validate mapping tags: %w", err)
	}
	if invalidTagSnapshots != 0 {
		return ErrMapping
	}
	return nil
}

func queuedImportInput(summary Summary, root Root, expectedVersion int64, executionID string) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_EMULATIONSTATION_IMPORT",
		"scope":         map[string]any{"type": "EMULATIONSTATION_IMPORT", "id": summary.ID},
		"executionId":   executionID,
		"inputs": map[string]any{
			"rootId":                summary.Root.ID,
			"sourceRelativePath":    summary.SourceRelativePath,
			"rootConfigDigest":      root.digest,
			"sourceSnapshotVersion": expectedVersion,
		},
	}
}

func createQueuedImportJob(
	ctx context.Context,
	transaction *sql.Tx,
	importID, jobID string,
	encoded []byte,
	now int64,
) error {
	digest := sha256.Sum256(encoded)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(
id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,
state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms
) VALUES(
?,'EMULATIONSTATION_IMPORT',?,'SERVER_EMULATIONSTATION_IMPORT',?,1,
'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?
)`, jobID, importID, jobDedupe("SERVER_EMULATIONSTATION_IMPORT", importID), now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/create import job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, jobID, string(encoded), hex.EncodeToString(digest[:]), now); err != nil {
		return fmt.Errorf("emulationstationimport/create import input: %w", err)
	}
	return nil
}

func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	version int64,
	reason, userID string,
) (Summary, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Summary{}, false, ErrNotCancellable
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, false, fmt.Errorf("emulationstationimport/cancel transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var actual int64
	var jobID sql.NullString
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version,import_job_id FROM emulationstation_imports WHERE id=?`, importID,
	).Scan(&state, &actual, &jobID); err != nil ||
		actual != version ||
		!jobID.Valid ||
		state != "QUEUED" && state != "RUNNING" {
		return Summary{}, false, ErrNotCancellable
	}
	now := service.now().UnixMilli()
	pending := state == "RUNNING"
	if err := persistCancellation(ctx, transaction, cancellation{
		ImportID: importID,
		JobID:    jobID.String,
		Reason:   reason,
		UserID:   userID,
		Now:      now,
		Pending:  pending,
	}); err != nil {
		return Summary{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, false, fmt.Errorf("emulationstationimport/commit cancel: %w", err)
	}
	result, err := service.Get(ctx, importID)
	return result, pending, err
}

type cancellation struct {
	ImportID string
	JobID    string
	Reason   string
	UserID   string
	Now      int64
	Pending  bool
}

func persistCancellation(ctx context.Context, transaction *sql.Tx, value cancellation) error {
	newState, jobState := "CANCELLED", "CANCELLED"
	var completed any = value.Now
	if value.Pending {
		newState, jobState, completed = "CANCEL_REQUESTED", "CANCEL_REQUESTED", nil
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_id=? AND execution_state='PENDING'`, value.Now, value.Now, value.ImportID); err != nil {
		return fmt.Errorf("emulationstationimport/cancel pending items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=?`, jobState, value.Now, value.Reason, completed, value.Now, value.JobID); err != nil {
		return fmt.Errorf("emulationstationimport/cancel job: %w", err)
	}
	if err := persistCancellationAggregate(
		ctx,
		transaction,
		value,
		newState,
		completed,
	); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'CANCEL_REQUESTED','{"schemaVersion":1}',?)`,
		value.JobID, value.ImportID, value.Now); err != nil {
		return fmt.Errorf("emulationstationimport/create cancel event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(
?,'USER',?,NULL,'EMULATIONSTATION_IMPORT_CANCEL_REQUESTED','EMULATIONSTATION_IMPORT',
?,'{}','{}',NULL,NULL,?
)`, auditID.String(), value.UserID, value.ImportID, value.Now); err != nil {
		return fmt.Errorf("emulationstationimport/create cancel audit: %w", err)
	}
	if err := scheduleTerminalItems(ctx, transaction, value.ImportID, value.Now); err != nil {
		return err
	}
	return nil
}

func persistCancellationAggregate(
	ctx context.Context,
	transaction *sql.Tx,
	value cancellation,
	state string,
	completed any,
) error {
	if value.Pending {
		_, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_imports
SET state=?,cancel_reason=?,completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, state, value.Reason, value.Now, value.ImportID)
		if err != nil {
			return fmt.Errorf("emulationstationimport/request cancel import: %w", err)
		}
		return nil
	}
	counts, err := loadTerminalItemCounts(ctx, transaction, value.ImportID)
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `
UPDATE emulationstation_imports
SET state=?,cancel_reason=?,
skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,
review_discarded_item_count=?,existing_item_count=?,blocked_item_count=?,
failed_item_count=?,cancelled_item_count=?,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`,
		state,
		value.Reason,
		counts.SkippedMapping,
		counts.ReviewPending,
		counts.Published,
		counts.ReviewDiscarded,
		counts.Existing,
		counts.Blocked,
		counts.Failed,
		counts.Cancelled,
		completed,
		value.Now,
		value.ImportID,
	)
	if err != nil {
		return fmt.Errorf("emulationstationimport/cancel import: %w", err)
	}
	return nil
}

func (service *Service) Delete(ctx context.Context, importID string, expectedVersion int64) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/delete transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, scanJob string
	var version int64
	var importJob sql.NullString
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,scan_job_id,import_job_id,version FROM emulationstation_imports WHERE id=?`, importID,
	).Scan(&state, &scanJob, &importJob, &version); err != nil {
		return ErrNotFound
	}
	if version != expectedVersion || state != "AWAITING_MAPPING" && state != "EXPIRED" || importJob.Valid {
		return ErrInvalid
	}
	for _, statement := range []string{
		`DELETE FROM emulationstation_import_item_assets
WHERE item_id IN (SELECT id FROM emulationstation_import_items WHERE import_id=?)`,
		`DELETE FROM emulationstation_import_item_files
WHERE item_id IN (SELECT id FROM emulationstation_import_items WHERE import_id=?)`,
		`DELETE FROM emulationstation_import_items WHERE import_id=?`,
		`DELETE FROM emulationstation_collection_tags
WHERE collection_id IN (SELECT id FROM emulationstation_import_collections WHERE import_id=?)`,
		`DELETE FROM emulationstation_import_collections WHERE import_id=?`,
		`DELETE FROM emulationstation_import_gamelists WHERE import_id=?`,
		`DELETE FROM emulationstation_imports WHERE id=?`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, importID); err != nil {
			return fmt.Errorf("emulationstationimport/delete plan: %w", err)
		}
	}
	// Immutable job/input/event evidence intentionally remains after the plan's
	// mutable scan projection is removed.
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit delete: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/start expiry transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='CANCELLED',error_code='EMULATIONSTATION_PLAN_EXPIRED',retryable=0,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE execution_state='PENDING' AND import_id IN (
 SELECT id FROM emulationstation_imports WHERE state='AWAITING_MAPPING' AND expires_at_ms<=?
)`, now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/expire plan items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE emulationstation_imports
SET state='EXPIRED',phase=NULL,last_error_code='EMULATIONSTATION_PLAN_EXPIRED',
cancelled_item_count=(
 SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state='AWAITING_MAPPING' AND expires_at_ms<=?`,
		now,
		now,
		now,
	); err != nil {
		return fmt.Errorf("emulationstationimport/expire plans: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit expired plans: %w", err)
	}
	return nil
}

func (service *Service) Retry(ctx context.Context, importID string, version int64, userID string) (Summary, error) {
	_ = userID
	summary, err := service.retrySummary(ctx, importID, version)
	if err != nil {
		return Summary{}, err
	}
	if err := service.revalidateRetryInputs(ctx, summary); err != nil {
		return Summary{}, err
	}
	if err := service.queueRetryExecution(ctx, summary, version); err != nil {
		return Summary{}, err
	}
	service.signal()
	return service.Get(ctx, importID)
}

func (service *Service) retrySummary(ctx context.Context, importID string, version int64) (Summary, error) {
	summary, err := service.Get(ctx, importID)
	if err != nil || summary.Version != version || !summary.Retryable || summary.ImportJobID == nil ||
		summary.State != "FAILED" && summary.State != "PARTIAL_FAILURE" {
		return Summary{}, ErrNotRetryable
	}
	return summary, nil
}

func (service *Service) revalidateRetryInputs(ctx context.Context, summary Summary) error {
	root, err := service.resolveStartRoot(ctx, summary.ID, summary.Root.ID)
	if err != nil {
		return err
	}
	if err := service.verifySnapshot(ctx, summary.ID, summary.SourceRelativePath, root); err != nil {
		return err
	}
	return service.verifyMappingTargets(ctx, summary.ID)
}

func (service *Service) queueRetryExecution(ctx context.Context, summary Summary, version int64) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/retry transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var execution int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT execution_no FROM jobs WHERE id=?`, *summary.ImportJobID,
	).Scan(&execution); err != nil {
		return ErrNotRetryable
	}
	execution++
	now := service.now().UnixMilli()
	reset, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE import_id=?
AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')`, now, summary.ID)
	if err != nil {
		return fmt.Errorf("emulationstationimport/reset retryable items: %w", err)
	}
	if rowsAffected(reset) == 0 {
		return ErrNotRetryable
	}
	inputID, _ := uuid.NewV7()
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_EMULATIONSTATION_IMPORT",
		"scope":         map[string]any{"type": "EMULATIONSTATION_IMPORT", "id": summary.ID},
		"executionId":   inputID.String(),
		"inputs":        map[string]any{"retry": true, "version": version},
	}
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)`, *summary.ImportJobID, execution, string(encoded), hex.EncodeToString(digest[:]), now); err != nil {
		return fmt.Errorf("emulationstationimport/create retry input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',execution_no=?,payload_json=json_object('inputExecutionNo',?),
attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, execution, execution, now, now, *summary.ImportJobID); err != nil {
		return fmt.Errorf("emulationstationimport/queue retry job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_imports
SET state='QUEUED',phase=NULL,last_error_code=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, now, summary.ID); err != nil {
		return fmt.Errorf("emulationstationimport/queue retry import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(
?,'EMULATIONSTATION_IMPORT',?,'MANUAL_RETRY',
json_object('schemaVersion',1,'executionNo',?),?
)`, *summary.ImportJobID, summary.ID, execution, now); err != nil {
		return fmt.Errorf("emulationstationimport/create retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit retry: %w", err)
	}
	return nil
}

// Keep the imported time package tied to the seven-day contract in this file.
var _ = 7 * 24 * time.Hour
