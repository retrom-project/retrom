package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"
)

// ValidationJobs shares the enclosing Product, move or DAT transaction.
type ValidationJobs struct{ executor dbexec.Executor }

func NewValidationJobs(executor dbexec.Executor) *ValidationJobs {
	return &ValidationJobs{executor: executor}
}

func (repository *ValidationJobs) Find(ctx context.Context, dedupe string) (application.ValidationJob, bool, error) {
	var job application.ValidationJob
	var retryable *int64
	var snapshot *string
	err := repository.executor.QueryRowContext(ctx, `
SELECT job.id,job.state,job.error_retryable,job.execution_no,job.version,
 (SELECT input_json FROM job_input_snapshots WHERE job_id=job.id AND execution_no=job.execution_no)
FROM jobs job WHERE job.kind='VARIANT_VALIDATE' AND job.dedupe_key=?`, dedupe).Scan(
		&job.ID,
		&job.State,
		&retryable,
		&job.ExecutionNo,
		&job.Version,
		&snapshot,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ValidationJob{}, false, nil
	}
	if err != nil {
		return application.ValidationJob{}, false, fmt.Errorf("query validation job: %w", err)
	}
	job.Retryable = retryable != nil && *retryable == 1
	if snapshot != nil {
		job.SnapshotJSON = *snapshot
	}
	return job, true, nil
}

func (repository *ValidationJobs) Write(ctx context.Context, plan application.ValidationJobWrite) error {
	if plan.Retry {
		if err := repository.retry(ctx, plan); err != nil {
			return err
		}
	} else if _, err := repository.executor.ExecContext(
		ctx,
		`INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'GAME_VARIANT',?,'VARIANT_VALIDATE',?,1,?,0,'QUEUED',0,2,?,?,?)`,
		plan.JobID,
		plan.VariantID,
		plan.DedupeKey,
		plan.PayloadJSON,
		plan.NowMS,
		plan.NowMS,
		plan.NowMS,
	); err != nil {
		return fmt.Errorf("insert validation job: %w", err)
	}
	if _, err := repository.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)`, plan.JobID, plan.ExecutionNo, plan.SnapshotJSON, plan.InputDigest, plan.NowMS); err != nil {
		return fmt.Errorf("insert validation snapshot: %w", err)
	}
	event, data := "QUEUED", "{}"
	if plan.Retry {
		event = "RETRY_SCHEDULED"
		data = fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"trigger":"LAUNCH"}`, plan.ExecutionNo)
	}
	if _, err := repository.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,?,?,?)`, plan.JobID, plan.VariantID, event, data, plan.NowMS); err != nil {
		return fmt.Errorf("insert validation event: %w", err)
	}
	return nil
}

func (repository *ValidationJobs) retry(ctx context.Context, plan application.ValidationJobWrite) error {
	result, err := repository.executor.ExecContext(
		ctx,
		`UPDATE jobs
SET state='QUEUED',execution_no=?,payload_json=?,attempt_count=0,available_at_ms=?,
execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state='FAILED' AND error_retryable=1`,
		plan.ExecutionNo,
		plan.PayloadJSON,
		plan.NowMS,
		plan.NowMS,
		plan.JobID,
		plan.PreviousVersion,
	)
	if err != nil {
		return fmt.Errorf("reset validation job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count validation job changes: %w", err)
	}
	if affected != 1 {
		return application.ErrBlocked
	}
	return nil
}
