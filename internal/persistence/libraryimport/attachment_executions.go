package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

type (
	AttachmentExecutions      struct{ database dbapi.DB }
	attachmentRecoveryRecords struct{ executor dbapi.Executor }
)

func NewAttachmentExecutions(database dbapi.DB) *AttachmentExecutions {
	return &AttachmentExecutions{database: database}
}

func (repository *AttachmentExecutions) WithRecovery(
	ctx context.Context, run func(libraryservice.AttachmentRecoveryRecords) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attachment recovery: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(attachmentRecoveryRecords{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attachment recovery: %w", err)
	}
	return nil
}

const (
	attachmentJobs    = ` FROM jobs job WHERE job.kind IN ('REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE')`
	attachmentColumns = `SELECT job.id,job.kind,job.scope_id,job.state,COALESCE(job.worker_id,''),
job.execution_no,job.attempt_count,job.max_attempts,job.version,job.available_at_ms,
job.leased_until_ms,job.execution_deadline_at_ms,job.cancellable,job.error_retryable`
)

func (records attachmentRecoveryRecords) Current(
	ctx context.Context, id string,
) (libraryservice.AttachmentExecution, error) {
	jobs, err := readAttachmentExecutions(ctx, records.executor, attachmentColumns+attachmentJobs+" AND job.id=?", id)
	if err != nil {
		return libraryservice.AttachmentExecution{}, err
	}
	if len(jobs) != 1 {
		return libraryservice.AttachmentExecution{}, libraryservice.ErrInvalid
	}
	return jobs[0], nil
}

func (repository *AttachmentExecutions) Queued(
	ctx context.Context, now int64,
) ([]libraryservice.AttachmentExecution, error) {
	return readAttachmentExecutions(ctx, repository.database, attachmentColumns+attachmentJobs+`
AND job.state='QUEUED' AND job.available_at_ms<=? AND job.attempt_count<job.max_attempts
AND (job.execution_deadline_at_ms IS NULL OR job.execution_deadline_at_ms>?)
ORDER BY job.available_at_ms,job.id LIMIT 64`, now, now)
}

func (records attachmentRecoveryRecords) Interrupted(
	ctx context.Context, now int64,
) ([]libraryservice.AttachmentExecution, error) {
	return readAttachmentExecutions(ctx, records.executor, attachmentColumns+attachmentJobs+` AND (
job.state IN ('RUNNING','CANCEL_REQUESTED')
AND (COALESCE(job.leased_until_ms,0)<=? OR job.execution_deadline_at_ms<=?) OR
job.state='QUEUED' AND (job.attempt_count>=job.max_attempts OR job.execution_deadline_at_ms<=?) OR
job.state='CANCELLED' AND (
EXISTS(SELECT 1 FROM review_arcade_parent_attachments WHERE job_id=job.id AND state='PENDING') OR
EXISTS(SELECT 1 FROM review_multidisc_attachments WHERE job_id=job.id AND state='PENDING')))
ORDER BY job.id LIMIT 64`, now, now, now)
}

func readAttachmentExecutions(
	ctx context.Context, executor dbapi.Executor, query string, args ...any,
) ([]libraryservice.AttachmentExecution, error) {
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attachment executions: %w", err)
	}
	defer func() { cleanup.Error("close attachment executions", rows.Close()) }()
	result := []libraryservice.AttachmentExecution{}
	for rows.Next() {
		var job libraryservice.AttachmentExecution
		if err := rows.Scan(&job.ID, &job.Kind, &job.ScopeID, &job.State, &job.WorkerID, &job.ExecutionNo,
			&job.Attempt, &job.MaxAttempts, &job.Version, &job.AvailableMS, &job.LeaseMS, &job.DeadlineMS,
			&job.Cancellable, &job.Retryable); err != nil {
			return nil, fmt.Errorf("scan attachment execution: %w", err)
		}
		result = append(result, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment executions: %w", err)
	}
	return result, nil
}

func (records attachmentRecoveryRecords) Transition(
	ctx context.Context, before libraryservice.AttachmentExecution, change libraryservice.AttachmentTransition,
) error {
	var finished *int64
	var code *string
	if change.State == "FAILED" || change.State == "CANCELLED" {
		finished = &change.NowMS
	}
	if change.State == "FAILED" {
		code = &change.Code
	}
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state=?,available_at_ms=?,
worker_id=CASE WHEN ?='CANCEL_REQUESTED' THEN worker_id END,
leased_until_ms=CASE WHEN ?='CANCEL_REQUESTED' THEN leased_until_ms END,
heartbeat_at_ms=CASE WHEN ?='CANCEL_REQUESTED' THEN heartbeat_at_ms END,
cancel_requested_at_ms=CASE WHEN ? IN ('CANCEL_REQUESTED','CANCELLED') THEN COALESCE(cancel_requested_at_ms,?)
 ELSE cancel_requested_at_ms END,
error_code=?,error_retryable=?,finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND kind=? AND state=? AND execution_no=?
AND attempt_count=? AND version=? AND COALESCE(worker_id,'')=?`,
		change.State, change.AvailableMS, change.State, change.State, change.State, change.State, change.NowMS,
		code, change.Retryable, finished, change.NowMS,
		before.ID, before.Kind, before.State, before.ExecutionNo, before.Attempt, before.Version, before.WorkerID)
	if err := requireAttachmentChange(result, err, "recover attachment execution"); err != nil {
		return err
	}
	if change.State == "CANCELLED" {
		update := recordstore.Update{
			Set:    `state='CANCELLED',error_code='CANCELLED',finished_at_ms=?,version=version+1,updated_at_ms=?`,
			Scope:  recordstore.Scope{Where: `job_id=? AND state='PENDING'`, Args: []any{before.ID}},
			Values: []any{change.NowMS, change.NowMS},
		}
		if before.Kind == "REVIEW_ARCADE_PARENT_VALIDATE" {
			_, err = recordstore.UpdateReviewArcadeParentAttachments(ctx, records.executor, update)
		} else {
			_, err = recordstore.UpdateReviewMultidiscAttachments(ctx, records.executor, update)
		}
		if err != nil {
			return fmt.Errorf("recover attachment cancellation: %w", err)
		}
	}
	if change.Event != "" {
		event, err := json.Marshal(map[string]any{
			"schemaVersion": 1, "executionNo": before.ExecutionNo, "attempt": before.Attempt,
			"errorCode": change.Code, "retryAtMs": change.AvailableMS,
			"reason": change.Reason,
		})
		if err != nil {
			return fmt.Errorf("encode attachment recovery event: %w", err)
		}
		_, err = records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,?,?,?)`, before.ID, before.ScopeID, change.Event, string(event), change.NowMS)
		if err != nil {
			return fmt.Errorf("record attachment recovery: %w", err)
		}
	}
	return nil
}

func requireAttachmentWorker(
	ctx context.Context, executor dbapi.Executor, jobID, workerID string, now int64, success bool,
) error {
	var state string
	var lease, deadline sql.NullInt64
	err := dbapi.QueryRowContext(ctx, executor, `
SELECT state,leased_until_ms,execution_deadline_at_ms FROM jobs WHERE id=? AND worker_id=?
`, jobID, workerID).Scan(&state, &lease, &deadline)
	if err != nil {
		return fmt.Errorf("attachment execution owner: %w", err)
	}
	if state != "RUNNING" || !lease.Valid || lease.Int64 <= now || success && (!deadline.Valid || deadline.Int64 <= now) {
		return libraryservice.ErrInvalid
	}
	return nil
}

func HeartbeatAttachment(ctx context.Context, executor dbapi.Executor, jobID, workerID string, now int64) error {
	result, err := executor.ExecContext(ctx, `UPDATE jobs
SET leased_until_ms=MIN(?,execution_deadline_at_ms),heartbeat_at_ms=?,updated_at_ms=?
WHERE id=? AND worker_id=? AND state='RUNNING' AND leased_until_ms>? AND execution_deadline_at_ms>?
`, now+60_000, now, now, jobID, workerID, now, now)
	return requireAttachmentChange(result, err, "heartbeat attachment execution")
}

func claimAttachmentRecords(
	ctx context.Context, tx dbapi.Tx, jobID, workerID string, now int64, kind string, durationMS int64,
) error {
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),
leased_until_ms=MIN(?,COALESCE(execution_deadline_at_ms,?)),heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind=? AND state='QUEUED' AND available_at_ms<=? AND attempt_count<max_attempts
AND (execution_deadline_at_ms IS NULL OR execution_deadline_at_ms>?)`, workerID, now, now+durationMS,
		now+60_000, now+durationMS, now, now, jobID, kind, now, now)
	if err := requireAttachmentChange(result, err, "claim attachment execution"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'STARTED',
json_object('schemaVersion',1,'executionNo',execution_no,'attempt',attempt_count),?
FROM jobs WHERE id=?`, now, jobID)
	if err != nil {
		return fmt.Errorf("record attachment start: %w", err)
	}
	return nil
}

func requireAttachmentChange(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", action, err)
	}
	if changed != 1 {
		return libraryservice.ErrInvalid
	}
	return nil
}
