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

func (records workflowRecords) Retry(ctx context.Context, plan application.RetryPlan) error {
	reset, err := recordstore.UpdateEmulationstationImportItems(ctx, records.transaction, recordstore.Update{
		Set: `execution_state='PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `import_id=? AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')`, Args: []any{plan.Before.Summary.ID}},
		Values: []any{plan.NowMS},
	})
	if err != nil {
		return fmt.Errorf("reset retryable EmulationStation items: %w", err)
	}
	count, err := reset.RowsAffected()
	if err != nil {
		return fmt.Errorf("read EmulationStation retry item count: %w", err)
	}
	if count == 0 || count != plan.Before.RetryableItems {
		return application.ErrNotRetryable
	}
	if err := records.retryInput(ctx, plan); err != nil {
		return err
	}
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='QUEUED',execution_no=?,
payload_json=json_object('inputExecutionNo',?),attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,finished_at_ms=NULL,worker_id=NULL,
error_code=NULL,error_retryable=NULL,cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state=? AND version=? AND execution_no=?
AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND kind='SERVER_EMULATIONSTATION_IMPORT'`,
		plan.Execution, plan.Execution, plan.NowMS, plan.NowMS, *plan.Before.Summary.ImportJobID, plan.Before.JobState,
		plan.Before.JobVersion, plan.Before.Execution, plan.Before.Summary.ID)
	if err := requireWorkflowChange(result, err, application.ErrNotRetryable); err != nil {
		return err
	}
	if err := records.retryAggregate(ctx, plan); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'MANUAL_RETRY',json_object('schemaVersion',1,'executionNo',?),?)`,

		*plan.Before.Summary.ImportJobID,
		plan.Before.Summary.ID,
		plan.Execution,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("record EmulationStation retry event: %w", err)
	}
	return records.audit(
		ctx,
		workflowAudit{
			ID:       plan.AuditID,
			ActorID:  plan.ActorID,
			ImportID: plan.Before.Summary.ID,
			Action:   "EMULATIONSTATION_IMPORT_RETRIED",
			NowMS:    plan.NowMS,
		},
	)
}

func (records workflowRecords) retryInput(ctx context.Context, plan application.RetryPlan) error {
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_EMULATIONSTATION_IMPORT",
		"scope":       map[string]any{"type": "EMULATIONSTATION_IMPORT", "id": plan.Before.Summary.ID},
		"executionId": plan.ExecutionID, "inputs": map[string]any{"retry": true, "version": plan.Before.Summary.Version},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode EmulationStation retry input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)`,
		*plan.Before.Summary.ImportJobID,
		plan.Execution,
		string(encoded),
		hex.EncodeToString(digest[:]),
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("record EmulationStation retry input: %w", err)
	}
	return nil
}

func (records workflowRecords) retryAggregate(ctx context.Context, plan application.RetryPlan) error {
	counts, err := LoadTerminalItemCounts(ctx, records.executor, plan.Before.Summary.ID)
	if err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.transaction, recordstore.Update{
		Set: `state='QUEUED',phase=NULL,last_error_code=NULL,retryable=0,completed_at_ms=NULL,
version=version+1,updated_at_ms=?,
skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,review_discarded_item_count=?,
existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND state=? AND import_job_id=? AND mapping_version=?
AND root_config_digest=? AND source_snapshot_digest=? AND release_year_max=?
AND NOT EXISTS(SELECT 1 FROM emulationstation_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))`, Args: []any{
			plan.Before.Summary.ID, plan.Before.Summary.Version, plan.Before.Summary.State, *plan.Before.Summary.ImportJobID,
			plan.Before.Summary.MappingVersion, plan.Before.RootConfigDigest, plan.Before.SourceSnapshotDigest,
			plan.Before.ReleaseYearMax, plan.Before.Summary.ID,
		}},
		Values: append([]any{plan.NowMS}, terminalCountValues(counts)...),
	})
	return requireWorkflowChange(result, err, application.ErrNotRetryable)
}
