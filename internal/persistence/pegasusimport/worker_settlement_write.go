package pegasusimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records workerSettlementRecords) Close(ctx context.Context, change application.WorkerSettlementChange) error {
	before := change.Before
	result, err := records.tx.ExecContext(ctx, `UPDATE jobs SET state=?,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,worker_id=NULL,error_code=?,error_retryable=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state=? AND worker_id=? AND execution_no=? AND attempt_count=?
AND scope_type='PEGASUS_IMPORT' AND scope_id=? AND kind=? AND leased_until_ms=? AND leased_until_ms>?
AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=jobs.id)
OR(jobs.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)))`,
		change.State, change.NowMS, optionalText(change.Failure.Code), change.Failure.Retryable, change.NowMS,
		before.JobID, before.JobVersion, before.JobState, before.WorkerID, before.ExecutionNo, before.Attempt,
		before.ImportID, before.Kind, before.LeaseUntilMS, change.NowMS, before.DeadlineMS, change.NowMS,
		before.ImportID, before.ImportVersion, before.ImportState)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	version := before.ImportVersion
	if before.Kind == "SERVER_PEGASUS_SCAN" {
		if err := clearUnpublishedScan(ctx, records.tx, before.ImportID); err != nil {
			return err
		}
	}
	if before.Kind == "SERVER_PEGASUS_IMPORT" {
		if err := records.items(ctx, change); err != nil {
			return err
		}
		if err := refreshCounts(ctx, records.tx, before.ImportID, change.NowMS); err != nil {
			return err
		}
		version++
	}
	result, err = recordstore.UpdatePegasusImports(ctx, records.tx, recordstore.Update{
		Set:    `state=?,phase=NULL,last_error_code=?,retryable=?,completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{change.State, optionalText(change.Failure.Code), change.Failure.Retryable, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{before.ImportID, version, before.ImportState},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	if err := ScheduleTerminalItems(ctx, records.tx, before.ImportID, change.NowMS); err != nil {
		return err
	}
	return records.event(ctx, change)
}

func (records workerSettlementRecords) items(ctx context.Context, change application.WorkerSettlementChange) error {
	state, code := "CANCELLED", "CANCELLED"
	if change.State == "FAILED" {
		state, code = "COMMIT_FAILED", change.Failure.Code
	}
	_, err := recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state=?,error_code=?,error_details_json=NULL,retryable=?,
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{state, code, change.Failure.Retryable, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')
AND NOT EXISTS(SELECT 1 FROM import_items bound WHERE bound.id=pegasus_import_items.library_import_item_id
AND bound.import_job_id=pegasus_import_items.library_import_job_id AND bound.state='REVIEW_PENDING')`,
			Args: []any{change.Before.ImportID},
		},
	})
	if err != nil {
		return fmt.Errorf("close unfinished Pegasus items: %w", err)
	}
	var unfinished int
	if err := records.tx.QueryRowContext(ctx, `SELECT count(*) FROM pegasus_import_items
WHERE import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`, change.Before.ImportID).Scan(
		&unfinished,
	); err != nil {
		return fmt.Errorf("verify Pegasus settlement items: %w", err)
	}
	if unfinished != 0 {
		return application.ErrVersionConflict
	}
	return nil
}

func (records workerSettlementRecords) event(ctx context.Context, change application.WorkerSettlementChange) error {
	event := `{"schemaVersion":1}`
	if change.State == "FAILED" {
		encoded, err := json.Marshal(struct {
			SchemaVersion int    `json:"schemaVersion"`
			Code          string `json:"code"`
			Retryable     bool   `json:"retryable"`
		}{1, change.Failure.Code, change.Failure.Retryable})
		if err != nil {
			return fmt.Errorf("encode Pegasus failure event: %w", err)
		}
		event = string(encoded)
	}
	if _, err := records.tx.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,?,?,?)`,
		change.Before.JobID,
		change.Before.ImportID,
		change.State,
		event,
		change.NowMS,
	); err != nil {
		return fmt.Errorf("record Pegasus settlement event: %w", err)
	}
	return nil
}
