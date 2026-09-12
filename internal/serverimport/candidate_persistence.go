package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	importservice "retrom/internal/service/serverimport"

	importpersistence "retrom/internal/persistence/serverimport"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/cleanup"
)

func (service *Service) progress(ctx context.Context, unit work, phase string, current, total int64) {
	service.workerError("progress", service.leases().Progress(ctx, unit, phase, current, total))
}

// Item outcome, selected candidate and progress event are committed together.
func (service *Service) completeItem(
	ctx context.Context,
	unit work,
	requirementID, state string,
	candidate *evaluatedCandidate,
	code string,
) {
	now := service.now().UnixMilli()
	var method, details any
	if candidate != nil {
		_, methodValue := selectedStatus(candidate)
		method = methodValue
		encoded, _ := json.Marshal(candidate.Details)
		details = string(encoded)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer dbexec.Rollback(transaction)
	if err := importpersistence.LockWorker(
		ctx,
		transaction,
		unit,
		service.now().UnixMilli(),
		importpersistence.RunningWorker,
	); err != nil {
		service.workerError("completeItem", err)
		return
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
		Set: `
state=?,match_method=?,selection_details_json=?,outcome_code=?,
previous_installation_id=?,new_installation_id=?,completed_at_ms=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')`,
			Args:  []any{unit.ImportID, requirementID},
		},
		Values: []any{state, method, details, code, nil, nil, now, now},
	}); err != nil {
		return
	}
	if candidate != nil {
		candidateState := candidate.State
		if state == "SOURCE_CHANGED" {
			candidateState = "SOURCE_CHANGED"
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_candidates SET state=?,not_selected_reason=?,updated_at_ms=? WHERE id=?
`, candidateState, code, now, candidate.ID); err != nil {
			return
		}
	}
	eventJSON, _ := json.Marshal(map[string]any{"schemaVersion": 1, "phase": "INSTALLING", "result": state})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)
`, unit.JobID, unit.ImportID, string(eventJSON), now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) cancelRequested(ctx context.Context, jobID string) bool {
	var state string
	return service.database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state) == nil &&
		(state == "CANCEL_REQUESTED" || state == "CANCELLED")
}

func (service *Service) pollCancellation(ctx context.Context, unit work) bool {
	err := service.leases().Heartbeat(ctx, unit)
	service.workerError("poll cancellation", err)
	return err != nil
}

// Terminal import/job/event state is committed as one transaction.
func (service *Service) finishTask(ctx context.Context, unit work) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer dbexec.Rollback(transaction)
	if err := importpersistence.LockWorker(
		ctx,
		transaction,
		unit,
		service.now().UnixMilli(),
		importpersistence.RunningWorker,
	); err != nil {
		service.workerError("finishTask", err)
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil {
		return
	}
	failed := counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] + counts["READ_FAILED"] +
		counts["INVALID_ARCHIVE"] + counts["COMMIT_FAILED"]
	state := "COMPLETED"
	if failed > 0 {
		state = "PARTIAL_FAILURE"
	}
	if err := updateTerminalImport(
		ctx, transaction, unit.ImportID, state, "QUEUEING_REVALIDATION", nil, counts, now,
	); err != nil {
		return
	}
	_, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=?,worker_id=NULL,
version=version+1,updated_at_ms=? WHERE id=?
`, now, now, now, unit.JobID)
	if err != nil {
		return
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'SUCCEEDED','{"schemaVersion":1}',?)`, unit.JobID, unit.ImportID, now)
	if err == nil {
		_ = transaction.Commit()
	}
}

// Failure item/import/job/event state is committed as one transaction.
func (service *Service) failTask(ctx context.Context, unit work, code string) {
	now := service.now().UnixMilli()
	retryable := 0
	if code == "SERVER_IMPORT_ROOT_UNAVAILABLE" || code == "INTERNAL_ERROR" {
		retryable = 1
	}
	if retryable == 1 && service.scheduleAutomaticRetry(ctx, unit, code, now) {
		return
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer dbexec.Rollback(transaction)
	if err := importpersistence.LockWorker(
		ctx,
		transaction,
		unit,
		service.now().UnixMilli(),
		workerFailureAccess(
			unit,
		),
	); err != nil {
		service.workerError("failTask", err)
		return
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
		Set: `state='COMMIT_FAILED',outcome_code=?,completed_at_ms=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `server_import_id=? AND state IN ('PENDING','EVALUATING')`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{code, now, now},
	}); err != nil {
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil || updateTerminalImport(ctx, transaction, unit.ImportID, "FAILED", "", &code, counts, now) != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=?,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, code, retryable, now, now, unit.JobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'FAILED',json_object('schemaVersion',1,'code',?),?)
`, unit.JobID, unit.ImportID, code, now); err == nil {
		_ = transaction.Commit()
	}
}

// Retry reset, lease release and retry event must remain one atomic state transition.
func (service *Service) scheduleAutomaticRetry(ctx context.Context, unit work, code string, now int64) bool {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer dbexec.Rollback(transaction)
	if err := importpersistence.LockWorker(
		ctx,
		transaction,
		unit,
		service.now().UnixMilli(),
		importpersistence.RunningWorker,
	); err != nil {
		service.workerError("scheduleAutomaticRetry", err)
		return false
	}
	var attempt, maximum int64
	var deadline sql.NullInt64
	var terminalItems int64
	if err := transaction.QueryRowContext(ctx, `
SELECT job.attempt_count,job.max_attempts,job.execution_deadline_at_ms,
 (SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=import.id
  AND item.state NOT IN ('PENDING','EVALUATING'))
FROM jobs job JOIN server_imports import ON import.job_id=job.id
WHERE job.id=? AND job.state='RUNNING'
`, unit.JobID).Scan(&attempt, &maximum, &deadline, &terminalItems); err != nil {
		return false
	}
	availableAt, retry := importservice.AutomaticRetryAt(attempt, maximum, terminalItems, deadline.Int64, now)
	if !retry {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM server_bios_import_candidates WHERE server_import_id=?
`, unit.ImportID); err != nil {
		return false
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='PENDING',candidate_count=0,match_method=NULL,
selection_details_json=NULL,previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,
completed_at_ms=NULL,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `server_import_id=?`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{now},
	}); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='QUEUED',phase=NULL,candidate_count=0,evaluated_item_count=0,
multi_candidate_item_count=0,skipped_special_count=0,skipped_unrepresentable_path_count=0,
last_error_code=NULL,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, unit.ImportID); err != nil {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
error_code=NULL,error_retryable=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, availableAt, now, unit.JobID); err != nil {
		return false
	}
	eventJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attempt": attempt, "retryAtMs": availableAt, "errorCode": code,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'RETRY_SCHEDULED',?,?)
`, unit.JobID, unit.ImportID, string(eventJSON), now); err != nil {
		return false
	}
	if err := transaction.Commit(); err != nil {
		return false
	}
	time.AfterFunc(time.Duration(availableAt-now)*time.Millisecond, service.signal)
	return true
}

// Cancellation item/import/job/event state is committed as one transaction.
func (service *Service) cancelTask(ctx context.Context, unit work) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer dbexec.Rollback(transaction)
	if err := importpersistence.LockWorker(
		ctx,
		transaction,
		unit,
		service.now().UnixMilli(),
		importpersistence.CancelledWorker,
	); err != nil {
		service.workerError("cancelTask", err)
		return
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',outcome_code='CANCELLED',
completed_at_ms=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `server_import_id=? AND state IN ('PENDING','EVALUATING')`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{now, now},
	}); err != nil {
		return
	}
	counts, err := itemStateCounts(ctx, transaction, unit.ImportID)
	if err != nil || updateTerminalImport(ctx, transaction, unit.ImportID, "CANCELLED", "", nil, counts, now) != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, now, now, now, unit.JobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'CANCELLED','{"schemaVersion":1}',?)
`, unit.JobID, unit.ImportID, now); err == nil {
		_ = transaction.Commit()
	}
}

func itemStateCounts(ctx context.Context, transaction *sql.Tx, importID string) (map[string]int64, error) {
	counts := make(map[string]int64)
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT state,count(*) FROM server_bios_import_items WHERE server_import_id=? GROUP BY state`,
		importID,
	)
	if err != nil {
		return nil, fmt.Errorf("query server import item state counts: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan server import item state count: %w", err)
		}
		counts[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server import item state counts: %w", err)
	}
	return counts, nil
}

func updateTerminalImport(
	ctx context.Context,
	transaction *sql.Tx,
	importID, state, phase string,
	code *string,
	counts map[string]int64,
	now int64,
) error {
	failed := counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] + counts["READ_FAILED"] +
		counts["INVALID_ARCHIVE"] + counts["COMMIT_FAILED"]
	var phaseValue any
	if phase != "" {
		phaseValue = phase
	}
	_, err := transaction.ExecContext(
		ctx,
		`UPDATE server_imports SET state=?,phase=?,last_error_code=?,
imported_matched_count=?,imported_warning_count=?,imported_missing_entry_count=?,not_found_count=?,
skipped_existing_count=?,skipped_not_better_count=?,same_bytes_count=?,failed_item_count=?,cancelled_item_count=?,
completed_at_ms=?,version=version+1,updated_at_ms=? WHERE id=?`,
		state,
		phaseValue,
		code,
		counts["IMPORTED_MATCHED"],
		counts["IMPORTED_WARNING"],
		counts["IMPORTED_MISSING_ENTRY"],
		counts["NOT_FOUND"],
		counts["SKIPPED_EXISTING"],
		counts["SKIPPED_NOT_BETTER"],
		counts["ALREADY_SAME_BYTES"],
		failed,
		counts["CANCELLED"],
		now,
		now,
		importID,
	)
	if err != nil {
		return fmt.Errorf("update terminal server import: %w", err)
	}
	return nil
}
