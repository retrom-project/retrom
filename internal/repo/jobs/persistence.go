package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/model/jobs"
	"retrom/internal/repo/dbexec"
)

type (
	Repository struct{ database *sql.DB }
	records    struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) CommitCancel(
	ctx context.Context, cmd jobs.CancelCommand,
) (jobs.CancelResult, error) {
	var result jobs.CancelResult
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		store := records{executor: exec}
		job, err := store.Get(ctx, cmd.JobID)
		if err != nil {
			return fmt.Errorf("read cancellation job: %w", err)
		}
		if job.Version != cmd.ExpectedVersion || !job.Cancellable ||
			!jobs.CancellableJobState(job.State, job.Retryable) {
			return jobs.ErrConflict
		}
		if job.Kind == "REVIEW_BULK_APPROVE" {
			return jobs.ErrRetryViaDomain
		}
		if hasDomainHandler(job.Kind, cmd.DomainHandlerFor) {
			result.NeedsDomain = true
			result.DomainJob = job
			return nil
		}
		pending := job.State == "RUNNING"
		state := "CANCELLED"
		var finishedAtMS *int64
		if pending {
			state = "CANCEL_REQUESTED"
		} else {
			finishedAtMS = &cmd.NowMS
		}
		event, err := json.Marshal(struct {
			Reason string `json:"reason"`
		}{Reason: cmd.Reason})
		if err != nil {
			return fmt.Errorf("encode cancellation event: %w", err)
		}
		change := jobs.Cancellation{
			JobID: cmd.JobID, ExpectedVersion: cmd.ExpectedVersion,
			State: state, Reason: cmd.Reason,
			AtMS: cmd.NowMS, FinishedAtMS: finishedAtMS,
			Event: event,
		}
		if err := store.Cancel(ctx, change); err != nil {
			return fmt.Errorf("cancel job: %w", err)
		}
		if job.Kind == "SERVER_BIOS_IMPORT" {
			if err := store.CancelServerImport(ctx, change); err != nil {
				return fmt.Errorf("cancel server import: %w", err)
			}
		}
		result.Result = jobs.Result{
			Kind: job.Kind, JobID: cmd.JobID,
			State: state, ExecutionNo: job.ExecutionNo,
			Version: job.Version + 1,
		}
		result.Pending = pending
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("jobs: commit cancel: %w", err)
	}
	return result, nil
}

func (repository *Repository) CommitRetry(
	ctx context.Context, cmd jobs.RetryCommand,
) (jobs.Result, error) {
	var result jobs.Result
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		store := records{executor: exec}
		job, err := store.Get(ctx, cmd.JobID)
		if err != nil {
			return fmt.Errorf("read retry job: %w", err)
		}
		if err := jobs.RetryEligibility(job, cmd.ExpectedVersion); err != nil {
			return fmt.Errorf("check retry eligibility: %w", err)
		}
		previous, err := store.Input(ctx, cmd.JobID, job.ExecutionNo)
		if err != nil {
			return fmt.Errorf("read retry input: %w", err)
		}
		change, res, err := jobs.BuildRetryWrite(
			cmd.JobID, cmd.ExpectedVersion, previous, job, cmd.NowMS, cmd.ExecutionID,
		)
		if err != nil {
			return fmt.Errorf("build retry write: %w", err)
		}
		if err := store.Retry(ctx, change); err != nil {
			return fmt.Errorf("retry job: %w", err)
		}
		result = res
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("jobs: commit retry: %w", err)
	}
	return result, nil
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

func hasDomainHandler(kind string, domainKinds []string) bool {
	for _, k := range domainKinds {
		if k == kind {
			return true
		}
	}
	return false
}
