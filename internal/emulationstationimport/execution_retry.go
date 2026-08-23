package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/cleanup"
)

type automaticRetryOutcome uint8

const (
	retryNotEligible automaticRetryOutcome = iota
	retryScheduled
	retryDeadlineExhausted
	retryAttemptsExhausted
)

func automaticRetryDelay(attempt int64) time.Duration {
	switch attempt {
	case 1:
		return time.Second
	case 2:
		return 5 * time.Second
	case 3:
		return 30 * time.Second
	default:
		return 120 * time.Second
	}
}

// scheduleAutomaticRetry keeps the frozen execution deadline and input snapshot.
// A worker failure may be retried automatically only before any item has reached
// a terminal outcome; partial COPY/VALIDATING progress remains available to lease
// recovery, which follows the separate recovery transaction in service.go.
func (service *Service) scheduleAutomaticRetry(
	ctx context.Context,
	unit work,
	code string,
	now int64,
) (automaticRetryOutcome, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return retryNotEligible, fmt.Errorf("emulationstationimport/start automatic retry: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var attempt, maximum, terminalItems int64
	var deadline sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT job.attempt_count,job.max_attempts,job.execution_deadline_at_ms,
 (SELECT count(*) FROM emulationstation_import_items item
  WHERE item.import_id=job.scope_id
  AND item.execution_state NOT IN ('PENDING','COPYING','VALIDATING'))
FROM jobs job
WHERE job.id=? AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.state='RUNNING'
`, unit.JobID).Scan(&attempt, &maximum, &deadline, &terminalItems); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return retryNotEligible, nil
		}
		return retryNotEligible, fmt.Errorf("emulationstationimport/read automatic retry state: %w", err)
	}
	if attempt >= maximum {
		return retryAttemptsExhausted, nil
	}
	delay := automaticRetryDelay(attempt)
	availableAt := now + delay.Milliseconds()
	if !deadline.Valid || availableAt >= deadline.Int64 {
		return retryDeadlineExhausted, nil
	}
	if terminalItems != 0 {
		return retryNotEligible, nil
	}
	if err := service.resetAggregateForAutomaticRetry(ctx, transaction, unit, now); err != nil {
		return retryNotEligible, err
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',available_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,finished_at_ms=NULL,error_code=NULL,error_retryable=NULL,
version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, availableAt, now, unit.JobID)
	if err != nil {
		return retryNotEligible, fmt.Errorf("emulationstationimport/queue automatic retry: %w", err)
	}
	if rowsAffected(result) != 1 {
		return retryNotEligible, nil
	}
	event, _ := json.Marshal(map[string]any{
		"schemaVersion":  1,
		"executionNo":    unit.ExecutionNo,
		"attempt":        attempt,
		"retryAtMs":      availableAt,
		"errorCode":      code,
		"errorRetryable": true,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'RETRY_SCHEDULED',?,?)
`, unit.JobID, unit.ImportID, string(event), now); err != nil {
		return retryNotEligible, fmt.Errorf("emulationstationimport/create automatic retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return retryNotEligible, fmt.Errorf("emulationstationimport/commit automatic retry: %w", err)
	}
	return retryScheduled, nil
}

func (service *Service) resetAggregateForAutomaticRetry(
	ctx context.Context,
	transaction *sql.Tx,
	unit work,
	now int64,
) error {
	statement := `
UPDATE emulationstation_imports
SET phase='DISCOVERING_GAMELISTS',last_error_code=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='SCANNING'`
	if unit.Kind == "SERVER_EMULATIONSTATION_IMPORT" {
		statement = `
UPDATE emulationstation_imports
SET state='QUEUED',phase=NULL,last_error_code=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`
	}
	result, err := transaction.ExecContext(ctx, statement, now, unit.ImportID)
	if err != nil {
		return fmt.Errorf("emulationstationimport/reset aggregate for automatic retry: %w", err)
	}
	if rowsAffected(result) != 1 {
		return fmt.Errorf("emulationstationimport/reset automatic retry aggregate: %w", errItemStateChanged)
	}
	return nil
}
