package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records workflowRecords) Cancel(ctx context.Context, plan application.CancellationPlan) error {
	before := plan.Before
	result, err := records.transaction.ExecContext(ctx, `
UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND execution_no=? AND state=?`, plan.State, plan.NowMS, plan.Reason, plan.CompletedAtMS,
		plan.NowMS, *before.Summary.ImportJobID, before.JobVersion, before.Execution, before.JobState)
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return err
	}
	if !plan.Pending {
		if err := records.cancelPendingItems(ctx, plan); err != nil {
			return err
		}
	}
	result, err = recordstore.UpdatePegasusImports(ctx, records.transaction, recordstore.Update{
		Set: `state=?,cancel_reason=?,cancelled_item_count=(SELECT count(*) FROM pegasus_import_items
WHERE import_id=? AND execution_state='CANCELLED'),completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND state=? AND import_job_id=?`, Args: []any{
			before.Summary.ID, before.Summary.Version, before.Summary.State, *before.Summary.ImportJobID,
		}},
		Values: []any{plan.State, plan.Reason, before.Summary.ID, plan.CompletedAtMS, plan.NowMS},
	})
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return err
	}
	if _, err := records.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'CANCEL_REQUESTED','{"schemaVersion":1}',?)`,
		*before.Summary.ImportJobID, before.Summary.ID, plan.NowMS); err != nil {
		return fmt.Errorf("record Pegasus cancellation event: %w", err)
	}
	if err := records.audit(ctx, workflowAudit{
		ID: plan.AuditID, ActorID: plan.ActorID, ImportID: before.Summary.ID,
		Action: "PEGASUS_IMPORT_CANCEL_REQUESTED", NowMS: plan.NowMS,
	}); err != nil {
		return err
	}
	return ScheduleTerminalItems(ctx, records.transaction, before.Summary.ID, plan.NowMS)
}

func (records workflowRecords) cancelPendingItems(ctx context.Context, plan application.CancellationPlan) error {
	_, err := recordstore.UpdatePegasusImportItems(ctx, records.transaction, recordstore.Update{
		Set:    `execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `import_id=? AND execution_state='PENDING'`, Args: []any{plan.Before.Summary.ID}},
		Values: []any{plan.NowMS, plan.NowMS},
	})
	if err != nil {
		return fmt.Errorf("cancel queued Pegasus items: %w", err)
	}
	return nil
}
