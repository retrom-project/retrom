package pegasusimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records workflowRecords) Retry(ctx context.Context, plan application.RetryPlan) error {
	if err := records.queueRetryJob(ctx, plan); err != nil {
		return err
	}
	if err := records.resetRetryableItems(ctx, plan); err != nil {
		return err
	}
	if err := records.retryInput(ctx, plan); err != nil {
		return err
	}
	result, err := recordstore.UpdatePegasusImports(ctx, records.transaction, recordstore.Update{
		Set: `state='QUEUED',phase=NULL,last_error_code=NULL,retryable=0,
failed_item_count=(SELECT count(*) FROM pegasus_import_items WHERE import_id=?
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
completed_at_ms=NULL,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND state=? AND import_job_id=? AND retryable=1
AND NOT EXISTS(SELECT 1 FROM pegasus_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))`, Args: []any{
			plan.Before.Summary.ID, plan.Before.Summary.Version, plan.Before.Summary.State,
			*plan.Before.Summary.ImportJobID, plan.Before.Summary.ID,
		}},
		Values: []any{plan.Before.Summary.ID, plan.NowMS},
	})
	if err := requireWorkflowChange(result, err, application.ErrNotRetryable); err != nil {
		return err
	}
	if _, err := records.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'MANUAL_RETRY',json_object('schemaVersion',1,'executionNo',?),?)`,
		*plan.Before.Summary.ImportJobID, plan.Before.Summary.ID, plan.Execution, plan.NowMS); err != nil {
		return fmt.Errorf("record Pegasus manual retry: %w", err)
	}
	return records.audit(ctx, workflowAudit{
		ID: plan.AuditID, ActorID: plan.ActorID, ImportID: plan.Before.Summary.ID,
		Action: "PEGASUS_IMPORT_RETRIED", NowMS: plan.NowMS,
	})
}

func (records workflowRecords) queueRetryJob(ctx context.Context, plan application.RetryPlan) error {
	result, err := records.transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',execution_no=?,payload_json=json_object('inputExecutionNo',?),
attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL,finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND execution_no=? AND state=?`, plan.Execution, plan.Execution, plan.NowMS, plan.NowMS,
		*plan.Before.Summary.ImportJobID, plan.Before.JobVersion, plan.Before.Execution, plan.Before.JobState)
	return requireWorkflowChange(result, err, application.ErrNotRetryable)
}

func (records workflowRecords) resetRetryableItems(ctx context.Context, plan application.RetryPlan) error {
	result, err := recordstore.UpdatePegasusImportItems(ctx, records.transaction, recordstore.Update{
		Set: `execution_state='PENDING',error_code=NULL,error_details_json=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `import_id=? AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')`, Args: []any{plan.Before.Summary.ID}},
		Values: []any{plan.NowMS},
	})
	if err != nil {
		return fmt.Errorf("reset retryable Pegasus items: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read reset Pegasus item count: %w", err)
	}
	if count != plan.Before.RetryableItems {
		return application.ErrNotRetryable
	}
	return nil
}

func (records workflowRecords) retryInput(ctx context.Context, plan application.RetryPlan) error {
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_PEGASUS_IMPORT",
		"scope": map[string]any{"type": "PEGASUS_IMPORT", "id": plan.Before.Summary.ID}, "executionId": plan.ExecutionID,
		"inputs": map[string]any{"retry": true, "version": plan.Before.Summary.Version},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode Pegasus retry input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	_, err = records.transaction.ExecContext(
		ctx,
		`
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)`,
		*plan.Before.Summary.ImportJobID,
		plan.Execution,
		string(encoded),
		hex.EncodeToString(digest[:]),
		plan.NowMS,
	)
	if err != nil {
		return fmt.Errorf("record Pegasus retry input: %w", err)
	}
	return nil
}
