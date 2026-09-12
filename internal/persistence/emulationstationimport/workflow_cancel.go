package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records workflowRecords) Cancel(ctx context.Context, plan application.CancellationPlan) error {
	jobID, kind := workflowJob(plan.Before.Summary)
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,
finished_at_ms=?,leased_until_ms=CASE WHEN ? THEN leased_until_ms ELSE NULL END,
heartbeat_at_ms=CASE WHEN ? THEN heartbeat_at_ms ELSE NULL END,
worker_id=CASE WHEN ? THEN worker_id ELSE NULL END,version=version+1,updated_at_ms=?
WHERE id=? AND state=? AND version=? AND execution_no=?
AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND kind=?
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=jobs.scope_id AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.import_job_id=jobs.id)))`,
		plan.State, plan.NowMS, plan.Reason, plan.CompletedAtMS, plan.Pending, plan.Pending, plan.Pending, plan.NowMS, jobID,
		plan.Before.JobState, plan.Before.JobVersion, plan.Before.Execution, plan.Before.Summary.ID, kind,
		plan.Before.Summary.Version, plan.Before.Summary.State)
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return err
	}
	if err := records.cancelQueuedProjection(ctx, plan); err != nil {
		return err
	}
	if err := records.cancelAggregate(ctx, plan); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'CANCEL_REQUESTED','{"schemaVersion":1}',?)`,
		jobID,
		plan.Before.Summary.ID,
		plan.NowMS,
	)
	if err := requireWorkflowChange(result, err, application.ErrNotCancellable); err != nil {
		return fmt.Errorf("record EmulationStation cancellation event: %w", err)
	}
	if err := records.audit(
		ctx,
		workflowAudit{
			ID:       plan.AuditID,
			ActorID:  plan.ActorID,
			ImportID: plan.Before.Summary.ID,
			Action:   "EMULATIONSTATION_IMPORT_CANCEL_REQUESTED",
			NowMS:    plan.NowMS,
		},
	); err != nil {
		return err
	}
	if plan.Before.Summary.ImportJobID == nil {
		return nil
	}
	return ScheduleTerminalItems(ctx, records.transaction, plan.Before.Summary.ID, plan.NowMS)
}

func (records workflowRecords) cancelAggregate(ctx context.Context, plan application.CancellationPlan) error {
	if plan.Before.Summary.ImportJobID == nil {
		return records.cancelScanAggregate(ctx, plan)
	}
	change := recordstore.Update{
		Set: `state=?,cancel_reason=?,completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND state=? AND version=? AND import_job_id=?`, Args: []any{
			plan.Before.Summary.ID, plan.Before.Summary.State, plan.Before.Summary.Version, *plan.Before.Summary.ImportJobID,
		}},
		Values: []any{plan.State, plan.Reason, plan.CompletedAtMS, plan.NowMS},
	}
	if !plan.Pending {
		counts, err := LoadTerminalItemCounts(ctx, records.executor, plan.Before.Summary.ID)
		if err != nil {
			return err
		}
		change.Set += `,phase=NULL,skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,
review_discarded_item_count=?,existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?`
		change.Values = append(change.Values, terminalCountValues(counts)...)
	}
	result, err := recordstore.UpdateEmulationstationImports(ctx, records.transaction, change)
	return requireWorkflowChange(result, err, application.ErrNotCancellable)
}

func (records workflowRecords) cancelQueuedProjection(ctx context.Context, plan application.CancellationPlan) error {
	if plan.Pending {
		return nil
	}
	if plan.Before.Summary.ImportJobID == nil {
		return clearUnpublishedScan(ctx, records.executor, plan.Before.Summary.ID)
	}
	var count int64
	if err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM emulationstation_import_items
WHERE import_id=? AND execution_state='PENDING'`, plan.Before.Summary.ID).Scan(
		&count,
	); err != nil {
		return fmt.Errorf("count queued EmulationStation cancellations: %w", err)
	}
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.transaction, recordstore.Update{
		Set: `execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state='PENDING'`,
			Args:  []any{plan.Before.Summary.ID},
		}, Values: []any{
			plan.NowMS,
			plan.NowMS,
		},
	})
	return requireRecoveryCount(result, err, count)
}
