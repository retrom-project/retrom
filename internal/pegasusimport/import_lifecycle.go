package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/serversource"
)

func (service *Service) verifySnapshot(ctx context.Context, importID, selectedPath string, root Root) error {
	rows, err := service.database.QueryContext(
		ctx,
		`SELECT relative_path,size_bytes,content_digest,source_facts_digest
FROM pegasus_import_metadata_files
WHERE import_id=?
ORDER BY relative_path`,
		importID,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/read metadata evidence: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	count := 0
	for rows.Next() {
		var path, expectedDigest, expectedFacts string
		var size int64
		if err := rows.Scan(&path, &size, &expectedDigest, &expectedFacts); err != nil {
			return ErrSourceChanged
		}
		file, before, err := serversource.OpenRelativeFile(root.path, selectedPath, path)
		if err != nil || before.Size() != size || serversource.FactsDigest(before) != expectedFacts {
			if file != nil {
				cleanup.Error("close", file.Close())
			}
			return ErrSourceChanged
		}
		hash := sha256.New()
		if _, err := file.WriteTo(hash); err != nil {
			cleanup.Error("close", file.Close())
			return ErrSourceChanged
		}
		after, err := file.Stat()
		cleanup.Error("close", file.Close())
		if err != nil || !serversource.SameFileFacts(before, after) ||
			hex.EncodeToString(hash.Sum(nil)) != expectedDigest {
			return ErrSourceChanged
		}
		count++
	}
	if err := rows.Err(); err != nil || count == 0 {
		return ErrSourceChanged
	}
	return nil
}

func (service *Service) StartImport(ctx context.Context, importID string, expectedVersion int64) (Summary, error) {
	summary, err := service.Get(ctx, importID)
	if err != nil {
		return Summary{}, err
	}
	if summary.State == "QUEUED" || summary.State == "RUNNING" || summary.State == "COMPLETED" ||
		summary.State == "PARTIAL_FAILURE" {
		return summary, nil
	}
	if summary.State != "AWAITING_MAPPING" || service.now().UnixMilli() >= summary.ExpiresAtMS {
		return Summary{}, ErrExpired
	}
	if summary.Version != expectedVersion {
		return Summary{}, ErrVersionConflict
	}
	if summary.Counts.MappedCollections+summary.Counts.SkippedCollections != summary.Counts.Collections {
		return Summary{}, ErrMapping
	}
	if summary.Counts.MappedCollections == 0 {
		return Summary{}, ErrNoSelection
	}
	root, ok := service.roots[summary.Root.ID]
	if !ok {
		return Summary{}, ErrSourceChanged
	}
	if err := service.verifySnapshot(ctx, importID, summary.SourceRelativePath, root); err != nil {
		return Summary{}, err
	}
	if err := service.queueImport(ctx, summary, root, expectedVersion); err != nil {
		return Summary{}, err
	}
	service.signal()
	return service.Get(ctx, importID)
}

func (service *Service) queueImport(
	ctx context.Context,
	summary Summary,
	root Root,
	expectedVersion int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version FROM pegasus_imports WHERE id=?`, summary.ID,
	).Scan(&state, &version); err != nil || state != "AWAITING_MAPPING" {
		return ErrMapping
	}
	if version != expectedVersion {
		return ErrVersionConflict
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
UPDATE pegasus_import_items
SET execution_state='SKIPPED_MAPPING',completed_at_ms=?,updated_at_ms=?
WHERE import_id=?
AND collection_id IN (
  SELECT id FROM pegasus_import_collections WHERE import_id=? AND mapping_action='SKIP'
)`, now, now, summary.ID, summary.ID); err != nil {
		return fmt.Errorf("pegasusimport/skip mapped items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state=CASE discovery_state
  WHEN 'BLOCKED_SOURCE' THEN 'BLOCKED_SOURCE'
  ELSE 'BLOCKED_CONTENT'
END,
completed_at_ms=?,updated_at_ms=?
WHERE import_id=?
AND execution_state='PENDING'
AND discovery_state!='READY'`, now, now, summary.ID); err != nil {
		return fmt.Errorf("pegasusimport/close discovery items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET import_job_id=?,state='QUEUED',phase=NULL,
blocked_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
version=version+1,updated_at_ms=?
WHERE id=?`, jobID.String(), summary.ID, now, summary.ID); err != nil {
		return fmt.Errorf("pegasusimport/queue import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(
?,'PEGASUS_IMPORT',?,'QUEUED',
'{"schemaVersion":1,"executionNo":1,"attempt":0}',?
)`, jobID.String(), summary.ID, now); err != nil {
		return fmt.Errorf("pegasusimport/queue event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit start: %w", err)
	}
	return nil
}

func validateQueuedMappingTags(ctx context.Context, transaction *sql.Tx, importID string) error {
	var invalidTagSnapshots int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM pegasus_import_collections collection
JOIN json_each(collection.tag_snapshot_json) entry
LEFT JOIN tags tag ON tag.id=json_extract(entry.value,'$.tagId') AND tag.status='ACTIVE'
WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND tag.id IS NULL
`, importID).Scan(&invalidTagSnapshots); err != nil {
		return fmt.Errorf("pegasusimport/validate mapping tags: %w", err)
	}
	if invalidTagSnapshots != 0 {
		return ErrMapping
	}
	return nil
}

func queuedImportInput(summary Summary, root Root, expectedVersion int64, executionID string) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_PEGASUS_IMPORT",
		"scope":         map[string]any{"type": "PEGASUS_IMPORT", "id": summary.ID},
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
?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,
'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?
)`, jobID, importID, jobDedupe("SERVER_PEGASUS_IMPORT", importID), now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/create import job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, jobID, string(encoded), hex.EncodeToString(digest[:]), now); err != nil {
		return fmt.Errorf("pegasusimport/create import input: %w", err)
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
		return Summary{}, false, fmt.Errorf("pegasusimport/cancel transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var actual int64
	var jobID sql.NullString
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version,import_job_id FROM pegasus_imports WHERE id=?`, importID,
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
		return Summary{}, false, fmt.Errorf("pegasusimport/commit cancel: %w", err)
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
UPDATE pegasus_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,updated_at_ms=?
WHERE import_id=? AND execution_state='PENDING'`, value.Now, value.Now, value.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/cancel pending items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=?`, jobState, value.Now, value.Reason, completed, value.Now, value.JobID); err != nil {
		return fmt.Errorf("pegasusimport/cancel job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state=?,cancel_reason=?,
cancelled_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, newState, value.Reason, value.ImportID, completed, value.Now, value.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/cancel import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'CANCEL_REQUESTED','{"schemaVersion":1}',?)`,
		value.JobID, value.ImportID, value.Now); err != nil {
		return fmt.Errorf("pegasusimport/create cancel event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(
?,'USER',?,NULL,'PEGASUS_IMPORT_CANCEL_REQUESTED','PEGASUS_IMPORT',
?,'{}','{}',NULL,NULL,?
)`, auditID.String(), value.UserID, value.ImportID, value.Now); err != nil {
		return fmt.Errorf("pegasusimport/create cancel audit: %w", err)
	}
	return nil
}

func (service *Service) Delete(ctx context.Context, importID string, expectedVersion int64) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/delete transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, scanJob string
	var version int64
	var importJob sql.NullString
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,scan_job_id,import_job_id,version FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&state, &scanJob, &importJob, &version); err != nil {
		return ErrNotFound
	}
	if version != expectedVersion || state != "AWAITING_MAPPING" && state != "EXPIRED" || importJob.Valid {
		return ErrInvalid
	}
	for _, statement := range []string{
		`DELETE FROM pegasus_import_item_assets WHERE item_id IN (SELECT id FROM pegasus_import_items WHERE import_id=?)`,
		`DELETE FROM pegasus_import_item_files WHERE item_id IN (SELECT id FROM pegasus_import_items WHERE import_id=?)`,
		`DELETE FROM pegasus_import_items WHERE import_id=?`,
		`DELETE FROM pegasus_import_collections WHERE import_id=?`,
		`DELETE FROM pegasus_import_metadata_files WHERE import_id=?`,
		`DELETE FROM pegasus_imports WHERE id=?`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, importID); err != nil {
			return fmt.Errorf("pegasusimport/delete plan: %w", err)
		}
	}
	// Immutable job/input/event evidence intentionally remains after the plan's
	// mutable scan projection is removed.
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit delete: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	now := service.now().UnixMilli()
	_, err := service.database.ExecContext(
		ctx,
		`UPDATE pegasus_imports
SET state='EXPIRED',phase=NULL,last_error_code='PEGASUS_PLAN_EXPIRED',
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state='AWAITING_MAPPING' AND expires_at_ms<=?`,
		now,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/expire plans: %w", err)
	}
	return nil
}

func (service *Service) Retry(ctx context.Context, importID string, version int64, userID string) (Summary, error) {
	_ = userID
	summary, err := service.Get(ctx, importID)
	if err != nil || summary.Version != version || !summary.Retryable || summary.ImportJobID == nil ||
		summary.State != "FAILED" && summary.State != "PARTIAL_FAILURE" {
		return Summary{}, ErrNotRetryable
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/retry transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var execution int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT execution_no FROM jobs WHERE id=?`, *summary.ImportJobID,
	).Scan(&execution); err != nil {
		return Summary{}, ErrNotRetryable
	}
	execution++
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
completed_at_ms=NULL,updated_at_ms=?
WHERE import_id=?
AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')`, now, importID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/reset retryable items: %w", err)
	}
	inputID, _ := uuid.NewV7()
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_PEGASUS_IMPORT",
		"scope":         map[string]any{"type": "PEGASUS_IMPORT", "id": importID},
		"executionId":   inputID.String(),
		"inputs":        map[string]any{"retry": true, "version": version},
	}
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)`, *summary.ImportJobID, execution, string(encoded), hex.EncodeToString(digest[:]), now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create retry input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',execution_no=?,payload_json=json_object('inputExecutionNo',?),
attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, execution, execution, now, now, *summary.ImportJobID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/queue retry job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state='QUEUED',phase=NULL,last_error_code=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, now, importID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/queue retry import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(
?,'PEGASUS_IMPORT',?,'MANUAL_RETRY',
json_object('schemaVersion',1,'executionNo',?),?
)`, *summary.ImportJobID, importID, execution, now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/commit retry: %w", err)
	}
	service.signal()
	return service.Get(ctx, importID)
}

// Keep the imported time package tied to the seven-day contract in this file.
var _ = 7 * 24 * time.Hour
