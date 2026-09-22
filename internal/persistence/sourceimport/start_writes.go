package sourceimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/sourceimport"
)

func (records startRecords) Queue(ctx context.Context, plan application.StartPlan) error {
	if err := records.insertJob(ctx, plan); err != nil {
		return err
	}
	if err := records.closeUnselectedItems(ctx, plan); err != nil {
		return err
	}
	result, err := recordstore.UpdateSourceImports(ctx, records.transaction, recordstore.Update{
		Set: `import_job_id=?,state='QUEUED',phase=NULL,retryable=0,
blocked_item_count=(SELECT count(*) FROM source_import_items WHERE import_id=?
AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND state='AWAITING_MAPPING' AND import_job_id IS NULL
AND root_config_digest=? AND source_snapshot_digest=? AND expires_at_ms>?
AND NOT EXISTS(SELECT 1 FROM source_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))`, Args: []any{
			plan.Before.Summary.ID, plan.Before.Summary.Version, plan.Before.RootConfigDigest,
			plan.Before.SourceSnapshotDigest, plan.NowMS, plan.Before.Summary.ID,
		}},
		Values: []any{plan.JobID, plan.Before.Summary.ID, plan.NowMS},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	if _, err := records.transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SOURCE_IMPORT',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)`,
		plan.JobID,
		plan.Before.Summary.ID,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("record Source start event: %w", err)
	}
	if err := workflowRecords(records).audit(ctx, workflowAudit{
		ID: plan.AuditID, ActorID: plan.ActorID,
		ImportID: plan.Before.Summary.ID, Action: "SOURCE_IMPORT_STARTED", NowMS: plan.NowMS,
	}); err != nil {
		return err
	}
	return nil
}

func (records startRecords) insertJob(ctx context.Context, plan application.StartPlan) error {
	if _, err := records.transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'SOURCE_IMPORT',?,'IMPORT_RECEIVE',?,1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?)`,
		plan.JobID, plan.Before.Summary.ID, plan.DedupeKey, plan.NowMS, plan.NowMS, plan.NowMS); err != nil {
		return fmt.Errorf("insert Source execution job: %w", err)
	}
	input := map[string]any{
		"schemaVersion": 1, "kind": "IMPORT_RECEIVE", "scope": map[string]any{
			"type": "SOURCE_IMPORT",
			"id":   plan.Before.Summary.ID,
		},
		"executionId": plan.ExecutionID, "inputs": map[string]any{
			"rootId":             plan.Before.Summary.Root.ID,
			"sourceRelativePath": plan.Before.Summary.SourceRelativePath, "rootConfigDigest": plan.Before.RootConfigDigest,
			"sourceSnapshotVersion": plan.Before.Summary.Version,
		},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode Source execution input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if _, err := records.transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, plan.JobID, string(encoded), hex.EncodeToString(digest[:]), plan.NowMS); err != nil {
		return fmt.Errorf("record Source execution input: %w", err)
	}
	return nil
}

func (records startRecords) closeUnselectedItems(ctx context.Context, plan application.StartPlan) error {
	if _, err := recordstore.UpdateSourceImportItems(ctx, records.transaction, recordstore.Update{
		Set: `execution_state='SKIPPED_MAPPING',completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `import_id=? AND collection_id IN
(SELECT id FROM source_import_collections WHERE import_id=? AND mapping_action='SKIP')
AND execution_state IN ('PENDING','BLOCKED_SOURCE','BLOCKED_CONTENT')`,
			Args: []any{plan.Before.Summary.ID, plan.Before.Summary.ID},
		},
		Values: []any{plan.NowMS, plan.NowMS},
	}); err != nil {
		return fmt.Errorf("close skipped Source items: %w", err)
	}
	if _, err := recordstore.UpdateSourceImportItems(ctx, records.transaction, recordstore.Update{
		Set: `execution_state=CASE discovery_state WHEN 'BLOCKED_SOURCE' THEN 'BLOCKED_SOURCE' ELSE 'BLOCKED_CONTENT' END,
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state='PENDING' AND discovery_state!='READY'`,
			Args:  []any{plan.Before.Summary.ID},
		},
		Values: []any{plan.NowMS, plan.NowMS},
	}); err != nil {
		return fmt.Errorf("close blocked Source discoveries: %w", err)
	}
	return nil
}
