package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/service/jobs"
)

type (
	Repository struct{ database *sql.DB }
	records    struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithWrite(ctx context.Context, work func(jobs.Records) error) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("jobs/begin: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(records{executor: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("jobs/commit: %w", err)
	}
	return nil
}

func (store records) Get(ctx context.Context, id string) (jobs.Job, error) {
	var job jobs.Job
	var cancellable int64
	var retryable sql.NullInt64
	err := store.executor.QueryRowContext(
		ctx,
		`
SELECT kind,scope_type,scope_id,state,cancellable,error_retryable,execution_no,version FROM jobs WHERE id=?
`,
		id,
	).Scan(
		&job.Kind,
		&job.ScopeType,
		&job.ScopeID,
		&job.State,
		&cancellable,
		&retryable,
		&job.ExecutionNo,
		&job.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, jobs.ErrConflict
	}
	if err != nil {
		return jobs.Job{}, fmt.Errorf("jobs/read: %w", err)
	}
	job.Cancellable, job.Retryable = cancellable == 1, retryable.Valid && retryable.Int64 == 1
	return job, nil
}

func (store records) Input(ctx context.Context, id string, executionNo int64) ([]byte, error) {
	var input []byte
	err := store.executor.QueryRowContext(ctx, `
SELECT input_json FROM job_input_snapshots WHERE job_id=? AND execution_no=?
`, id, executionNo).Scan(&input)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, jobs.ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("jobs/read input: %w", err)
	}
	return input, nil
}

func requireChanged(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("jobs/write state: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("jobs/count changes: %w", err)
	}
	if changed != 1 {
		return jobs.ErrConflict
	}
	return nil
}

func (store records) Cancel(ctx context.Context, change jobs.Cancellation) error {
	if err := requireChanged(
		store.executor.ExecContext(
			ctx,
			`
UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=?
`,
			change.State,
			change.AtMS,
			change.Reason,
			change.FinishedAtMS,
			change.AtMS,
			change.JobID,
			change.ExpectedVersion,
		),
	); err != nil {
		return err
	}
	if _, err := store.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,?,?,? FROM jobs WHERE id=?
`, change.State, string(change.Event), change.AtMS, change.JobID); err != nil {
		return fmt.Errorf("jobs/cancel event: %w", err)
	}
	return nil
}

func (store records) Retry(ctx context.Context, change jobs.RetryWrite) error {
	if _, err := store.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,?,?,?,?)
`, change.JobID, change.ExecutionNo, string(change.Input), change.InputDigest, change.AtMS); err != nil {
		return fmt.Errorf("jobs/retry snapshot: %w", err)
	}
	if err := requireChanged(
		store.executor.ExecContext(
			ctx,
			`
UPDATE jobs SET state='QUEUED',execution_no=?,payload_json=?,attempt_count=0,available_at_ms=?,
execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,cancel_requested_at_ms=NULL,cancel_reason=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND version=?
`,
			change.ExecutionNo,
			string(
				change.Payload,
			),
			change.AtMS,
			change.AtMS,
			change.JobID,
			change.ExpectedVersion,
		),
	); err != nil {
		return err
	}
	if _, err := store.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'MANUAL_RETRY',?,? FROM jobs WHERE id=?
`, string(change.Event), change.AtMS, change.JobID); err != nil {
		return fmt.Errorf("jobs/retry event: %w", err)
	}
	return nil
}
