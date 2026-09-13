package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records workflowRecords) Cancel(ctx context.Context, plan application.CancellationPlan) error {
	before := plan.Before
	jobID, kind := workflowJob(before.Summary)
	result, err := records.transaction.ExecContext(
		ctx,
		`UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,
finished_at_ms=?,leased_until_ms=CASE WHEN ? THEN leased_until_ms ELSE NULL END,
heartbeat_at_ms=CASE WHEN ? THEN heartbeat_at_ms ELSE NULL END,
worker_id=CASE WHEN ? THEN worker_id ELSE NULL END,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND execution_no=? AND state=? AND scope_type='PEGASUS_IMPORT' AND scope_id=? AND kind=?
AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=jobs.scope_id AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR(jobs.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=jobs.id)))`,

		plan.State,
		plan.NowMS,
		plan.Reason,
		plan.CompletedAtMS,
		plan.Pending,
		plan.Pending,
		plan.Pending,
		plan.NowMS,

		jobID,
		before.JobVersion,
		before.Execution,
		before.JobState,
		before.Summary.ID,
		kind,
		before.Summary.Version,
		before.Summary.State,
	)
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return err
	}
	if err := records.cancelQueuedProjection(ctx, plan); err != nil {
		return err
	}
	if err := records.cancelAggregate(ctx, plan); err != nil {
		return err
	}
	result, err = records.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'CANCEL_REQUESTED','{"schemaVersion":1}',?)`,
		jobID,
		before.Summary.ID,
		plan.NowMS,
	)
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return err
	}
	if err := records.audit(ctx, workflowAudit{
		ID: plan.AuditID, ActorID: plan.ActorID, ImportID: before.Summary.ID,
		Action: "PEGASUS_IMPORT_CANCEL_REQUESTED", NowMS: plan.NowMS,
	}); err != nil {
		return err
	}
	if before.Summary.ImportJobID == nil {
		return nil
	}
	return nil
}

func (records workflowRecords) cancelAggregate(ctx context.Context, plan application.CancellationPlan) error {
	before := plan.Before.Summary
	jobID, kind := workflowJob(before)
	result, err := recordstore.UpdatePegasusImports(ctx, records.transaction, recordstore.Update{
		Set: `state=?,phase=CASE WHEN ? THEN phase ELSE NULL END,cancel_reason=?,
cancelled_item_count=(SELECT count(*) FROM pegasus_import_items WHERE import_id=? AND execution_state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{plan.State, plan.Pending, plan.Reason, before.ID, plan.CompletedAtMS, plan.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?
AND ((?='SERVER_PEGASUS_SCAN' AND scan_job_id=? AND import_job_id IS NULL)
OR (?='SERVER_PEGASUS_IMPORT' AND import_job_id=?))`,
			Args: []any{before.ID, before.Version, before.State, kind, jobID, kind, jobID},
		},
	})
	return requireWorkflowChange(result, err, application.ErrNotCancellable)
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

func (records workflowRecords) cancelQueuedProjection(ctx context.Context, plan application.CancellationPlan) error {
	if plan.Pending {
		return nil
	}
	if plan.Before.Summary.ImportJobID == nil {
		return ClearUnpublishedScan(ctx, records.transaction, plan.Before.Summary.ID)
	}
	return records.cancelPendingItems(ctx, plan)
}
