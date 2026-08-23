package emulationstationimport

import (
	"context"
	"encoding/json"

	"retrom/internal/cleanup"
)

func (service *Service) persistExecutionFailure(
	ctx context.Context,
	unit work,
	code string,
	retryable bool,
	now int64,
) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	jobResult, err := transaction.ExecContext(
		ctx,
		`UPDATE jobs
SET state='FAILED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
error_code=?,error_retryable=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`,
		now,
		code,
		boolInt(retryable),
		now,
		unit.JobID,
	)
	if err != nil || rowsAffected(jobResult) != 1 {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='COMMIT_FAILED',error_code=?,retryable=?,completed_at_ms=?,
version=version+1,updated_at_ms=?
WHERE import_id=? AND execution_state IN ('PENDING','COPYING','VALIDATING')`,
		code, boolInt(retryable), now, now, unit.ImportID,
	); err != nil {
		return
	}
	if err := scheduleTerminalItems(ctx, transaction, unit.ImportID, now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(
		ctx,
		terminalFailureAggregateSQL,
		code,
		unit.ImportID,
		now,
		unit.ImportID,
		unit.ImportID,
		unit.ImportID,
		unit.ImportID,
		unit.ImportID,
		unit.ImportID,
		unit.ImportID,
		unit.ImportID,
		now,
		unit.ImportID,
	); err != nil {
		return
	}
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "code": code, "retryable": retryable})
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'FAILED',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	); err != nil {
		return
	}
	_ = transaction.Commit()
}

const terminalFailureAggregateSQL = `UPDATE emulationstation_imports
SET state='FAILED',phase=NULL,last_error_code=?,retryable=EXISTS(
 SELECT 1 FROM emulationstation_import_items
 WHERE import_id=?
 AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
 AND retryable=1
),completed_at_ms=?,
skipped_mapping_item_count=(
 SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='SKIPPED_MAPPING'
),
review_pending_item_count=(
 SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='REVIEW_PENDING'
),
published_item_count=(
 SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='PUBLISHED'
),
review_discarded_item_count=(
 SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='REVIEW_DISCARDED'
),
existing_item_count=(
 SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='SKIPPED_EXISTING'
),
blocked_item_count=(
 SELECT count(*) FROM emulationstation_import_items
 WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
failed_item_count=(
 SELECT count(*) FROM emulationstation_import_items
 WHERE import_id=? AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
cancelled_item_count=(
 SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='CANCELLED'
),
version=version+1,updated_at_ms=?
WHERE id=?`
