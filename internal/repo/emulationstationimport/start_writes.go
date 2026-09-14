package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/recordstore"
)

func (records startRecords) Queue(ctx context.Context, plan application.StartPlan) error {
	if err := records.insertJob(ctx, plan); err != nil {
		return err
	}
	if err := records.closeUnselectedItems(ctx, plan); err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.executor, recordstore.Update{
		Set: `import_job_id=?,state='QUEUED',phase=NULL,retryable=0,
skipped_mapping_item_count=(SELECT count(*) FROM emulationstation_import_items WHERE import_id=?
AND execution_state='SKIPPED_MAPPING'),
blocked_item_count=(SELECT count(*) FROM emulationstation_import_items WHERE import_id=?
AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND state='AWAITING_MAPPING' AND import_job_id IS NULL
AND mapping_version=? AND root_config_digest=? AND source_snapshot_digest=? AND release_year_max=? AND expires_at_ms>?
AND NOT EXISTS(SELECT 1 FROM emulationstation_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))`, Args: []any{
			plan.Before.Summary.ID, plan.Before.Summary.Version, plan.Before.Summary.MappingVersion,
			plan.Before.RootConfigDigest,
			plan.Before.SourceSnapshotDigest, plan.Before.ReleaseYearMax, plan.NowMS, plan.Before.Summary.ID,
		}},
		Values: []any{plan.JobID, plan.Before.Summary.ID, plan.Before.Summary.ID, plan.NowMS},
	})
	if err := requireMappingChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)`,
		plan.JobID,
		plan.Before.Summary.ID,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("record EmulationStation start event: %w", err)
	}
	if err := records.startAudit(ctx, plan); err != nil {
		return err
	}
	return nil
}

func (records startRecords) insertJob(ctx context.Context, plan application.StartPlan) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'SERVER_EMULATIONSTATION_IMPORT',?,1,
'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?)`,
		plan.JobID, plan.Before.Summary.ID, plan.DedupeKey, plan.NowMS, plan.NowMS, plan.NowMS); err != nil {
		return fmt.Errorf("insert EmulationStation execution job: %w", err)
	}
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_EMULATIONSTATION_IMPORT", "scope": map[string]any{
			"type": "EMULATIONSTATION_IMPORT",
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
		return fmt.Errorf("encode EmulationStation execution input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, plan.JobID, string(encoded), hex.EncodeToString(digest[:]), plan.NowMS); err != nil {
		return fmt.Errorf("record EmulationStation execution input: %w", err)
	}
	return nil
}

func (records startRecords) closeUnselectedItems(ctx context.Context, plan application.StartPlan) error {
	if _, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='SKIPPED_MAPPING',completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `import_id=? AND collection_id IN
(SELECT id FROM emulationstation_import_collections WHERE import_id=? AND mapping_action='SKIP')
AND execution_state IN ('PENDING','BLOCKED_SOURCE','BLOCKED_CONTENT')`,
			Args: []any{plan.Before.Summary.ID, plan.Before.Summary.ID},
		},
		Values: []any{plan.NowMS, plan.NowMS},
	}); err != nil {
		return fmt.Errorf("close skipped EmulationStation items: %w", err)
	}
	if _, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state=CASE discovery_state WHEN 'BLOCKED_SOURCE' THEN 'BLOCKED_SOURCE' ELSE 'BLOCKED_CONTENT' END,
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state='PENDING' AND discovery_state!='READY'`,
			Args:  []any{plan.Before.Summary.ID},
		},
		Values: []any{plan.NowMS, plan.NowMS},
	}); err != nil {
		return fmt.Errorf("close blocked EmulationStation discoveries: %w", err)
	}
	return nil
}

func (records startRecords) startAudit(ctx context.Context, plan application.StartPlan) error {
	_, err := records.executor.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'EMULATIONSTATION_IMPORT_STARTED','EMULATIONSTATION_IMPORT',?,
'{"state":"AWAITING_MAPPING"}','{"state":"QUEUED"}',NULL,NULL,?)`,
		plan.AuditID, plan.ActorID, plan.Before.Summary.ID, plan.NowMS)
	if err != nil {
		return fmt.Errorf("record EmulationStation start audit: %w", err)
	}
	return nil
}
