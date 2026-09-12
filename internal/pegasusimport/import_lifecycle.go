package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

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
		return Summary{}, fmt.Errorf("retry Pegasus import: %w", err)
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
	defer dbexec.Rollback(transaction)
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
	if _, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `execution_state='SKIPPED_MAPPING',completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `
import_id=?
AND collection_id IN (
  SELECT id FROM pegasus_import_collections WHERE import_id=? AND mapping_action='SKIP'
)
`,
			Args: []any{summary.ID, summary.ID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/skip mapped items: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state=CASE discovery_state
  WHEN 'BLOCKED_SOURCE' THEN 'BLOCKED_SOURCE'
  ELSE 'BLOCKED_CONTENT'
END,
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
import_id=?
AND execution_state='PENDING'
AND discovery_state!='READY'
`,
			Args: []any{summary.ID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/close discovery items: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: `
import_job_id=?,state='QUEUED',phase=NULL,
blocked_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{summary.ID},
		},
		Values: []any{jobID.String(), summary.ID, now},
	}); err != nil {
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
	if err := scheduleTerminalItems(ctx, transaction, summary.ID, now); err != nil {
		return err
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
	id string,
	version int64,
	reason, actorID string,
) (Summary, bool, error) {
	control := application.NewWorkflowControl(repository.NewWorkflowControl(service.database), service.now)
	result, pending, err := control.Cancel(ctx, id, version, reason, actorID)
	if err != nil {
		return Summary{}, false, fmt.Errorf("cancel Pegasus import: %w", err)
	}
	return result, pending, nil
}

func (service *Service) Delete(ctx context.Context, id string, version int64) error {
	actorID := ""
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	lifecycle := application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now)
	if err := lifecycle.Delete(ctx, id, version, actorID); err != nil {
		return fmt.Errorf("delete Pegasus import: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	lifecycle := application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now)
	if err := lifecycle.Expire(ctx); err != nil {
		return fmt.Errorf("expire Pegasus imports: %w", err)
	}
	return nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	control := application.NewWorkflowControl(repository.NewWorkflowControl(service.database), service.now)
	result, err := control.Retry(ctx, id, version, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("retry Pegasus import: %w", err)
	}
	service.signal()
	return result, nil
}
