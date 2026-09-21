package uploads

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	service "retrom/internal/service/uploads"
)

func (records jobRecords) Get(ctx context.Context, id string) (service.Job, error) {
	var result service.Job
	var worker, input, digest sql.NullString
	var deadline, lease sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `
SELECT job.id,job.state,job.kind,job.scope_type,job.scope_id,job.worker_id,job.execution_no,job.version,
job.attempt_count,job.max_attempts,job.execution_deadline_at_ms,job.leased_until_ms,job.available_at_ms,
snapshot.input_json,snapshot.input_digest FROM jobs job LEFT JOIN job_input_snapshots snapshot
ON snapshot.job_id=job.id AND snapshot.execution_no=job.execution_no WHERE job.id=?`, id).
		Scan(&result.ID, &result.State, &result.Kind, &result.Scope, &result.ScopeID, &worker,
			&result.ExecutionNo, &result.Version,
			&result.Attempt, &result.MaxAttempts, &deadline, &lease, &result.Available, &input, &digest)
	if err != nil {
		return service.Job{}, fmt.Errorf("read upload finalization job: %w", err)
	}
	result.WorkerID = worker.String
	result.Deadline = deadline.Int64
	result.Lease = lease.Int64
	result.Input = input.String
	result.InputDigest = digest.String
	return result, nil
}

func (records jobRecords) Claim(ctx context.Context, input service.JobClaim) (bool, error) {
	run, now := input.Run, input.AtMS
	result, err := records.executor.ExecContext(ctx, `
UPDATE jobs SET state=CASE WHEN state='CANCEL_REQUESTED' THEN state ELSE 'RUNNING' END,
worker_id=?,attempt_count=?,execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND execution_no=? AND version=? AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')`,
		run.WorkerID, run.Attempt, now, run.Deadline, min(now+60000, run.Deadline), now, now,
		run.JobID, run.ExecutionNo, input.Version)
	if err != nil {
		return false, fmt.Errorf("claim upload job: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count upload claim: %w", err)
	}
	if count != 1 {
		return false, nil
	}
	if err := records.event(ctx, run, "STARTED", input.EventJSON, now); err != nil {
		return false, err
	}
	return true, nil
}

func (repository *Repository) Recoverable(ctx context.Context, now int64) ([]string, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT job.id FROM jobs job JOIN upload_sessions session ON session.finalize_job_id=job.id
WHERE job.kind='UPLOAD_FINALIZE' AND job.scope_type='UPLOAD_SESSION' AND job.scope_id=session.id
AND (session.state='FINALIZING' OR (session.state='FAILED' AND job.state='QUEUED'))
AND (job.state='QUEUED' AND (job.available_at_ms<=? OR job.execution_deadline_at_ms<=?)
OR job.state IN ('RUNNING','CANCEL_REQUESTED') AND (COALESCE(job.leased_until_ms,0)<=?
OR job.execution_deadline_at_ms<=?) OR job.state='CANCELLED')
ORDER BY job.available_at_ms,job.id LIMIT 100`, now, now, now, now)
	if err != nil {
		return nil, fmt.Errorf("read upload recovery queue: %w", err)
	}
	defer func() { cleanup.Error("close upload recovery rows", rows.Close()) }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan upload recovery job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload recovery jobs: %w", err)
	}
	return ids, nil
}
