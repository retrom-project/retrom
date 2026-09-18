package emulationstationimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type Leases struct {
	database      *sql.DB
	preCommitHook func(dbexec.Executor) error
}

func NewLeases(database *sql.DB) *Leases { return &Leases{database: database} }

func (repository *Leases) WithPreCommitHook(
	hook func(dbexec.Executor) error,
) {
	repository.preCommitHook = hook
}

func (repository *Leases) LoadLeaseCandidate(
	ctx context.Context, now int64,
) (application.LeaseSnapshot, bool, error) {
	return scanLease(repository.database.QueryRowContext(ctx, leaseSnapshotSQL+`
AND job.state='QUEUED' AND job.available_at_ms<=? AND job.attempt_count<job.max_attempts
AND (job.execution_deadline_at_ms IS NULL OR job.execution_deadline_at_ms>?)
AND ((job.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.state='SCANNING')
OR (job.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.state='QUEUED'))
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1`, now, now))
}

func (repository *Leases) LoadCurrentLease(
	ctx context.Context, id string,
) (application.LeaseSnapshot, bool, error) {
	return scanLease(repository.database.QueryRowContext(
		ctx, leaseSnapshotSQL+` AND job.id=?`, id,
	))
}

func (repository *Leases) CommitLeaseClaim(
	ctx context.Context, change application.ClaimLease,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation lease claim: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := leaseRecords{executor: tx}
	if err := records.Claim(ctx, change); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation lease claim: %w", err)
	}
	return nil
}

func (repository *Leases) CommitLeaseRenewal(
	ctx context.Context, change application.RenewLease,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation lease renewal: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := leaseRecords{executor: tx}
	if err := records.Renew(ctx, change); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation lease renewal: %w", err)
	}
	return nil
}

type leaseRecords struct{ executor dbexec.Executor }

func (records leaseRecords) Next(ctx context.Context, now int64) (application.LeaseSnapshot, bool, error) {
	return scanLease(records.executor.QueryRowContext(ctx, leaseSnapshotSQL+`
AND job.state='QUEUED' AND job.available_at_ms<=? AND job.attempt_count<job.max_attempts
AND (job.execution_deadline_at_ms IS NULL OR job.execution_deadline_at_ms>?)
AND ((job.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.state='SCANNING')
OR (job.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.state='QUEUED'))
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1`, now, now))
}

func (records leaseRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return scanLease(records.executor.QueryRowContext(ctx, leaseSnapshotSQL+` AND job.id=?`, id))
}

func (records leaseRecords) Renew(ctx context.Context, change application.RenewLease) error {
	before := change.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND version=? AND state='RUNNING' AND kind=?
AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND execution_no=? AND attempt_count=? AND worker_id=?
AND leased_until_ms=? AND leased_until_ms>? AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.import_job_id=jobs.id)))`,
		change.NowMS, change.UntilMS, change.NowMS, before.JobID, before.JobVersion, before.Kind, before.ImportID,
		before.ExecutionNo, before.Attempt, before.WorkerID, before.LeaseUntilMS, change.NowMS, before.DeadlineAtMS,
		change.NowMS, before.ImportID, before.ImportVersion, before.ImportState)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

const leaseSnapshotSQL = `SELECT job.id,plan.id,job.kind,plan.root_id,plan.root_config_digest,
plan.source_relative_path,plan.created_by_user_id,plan.release_year_max,
COALESCE(job.worker_id,''),job.execution_no,job.attempt_count,COALESCE(job.execution_deadline_at_ms,0),
job.state,plan.state,job.version,plan.version,job.max_attempts,job.available_at_ms,
COALESCE(job.leased_until_ms,0),job.execution_started_at_ms
FROM jobs job JOIN emulationstation_imports plan ON plan.id=job.scope_id
WHERE job.scope_type='EMULATIONSTATION_IMPORT'
AND ((job.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.scan_job_id=job.id AND plan.import_job_id IS NULL)
OR (job.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.import_job_id=job.id))`

func scanLease(row dbexec.Scanner) (application.LeaseSnapshot, bool, error) {
	var value application.LeaseSnapshot
	err := row.Scan(&value.JobID, &value.ImportID, &value.Kind, &value.RootID, &value.RootDigest, &value.RelativePath,
		&value.CreatedByUserID, &value.ReleaseYearMax, &value.WorkerID,
		&value.ExecutionNo, &value.Attempt, &value.DeadlineAtMS,
		&value.JobState, &value.ImportState, &value.JobVersion, &value.ImportVersion,
		&value.MaxAttempts, &value.AvailableAtMS,
		&value.LeaseUntilMS, &value.StartedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.LeaseSnapshot{}, false, nil
	}
	if err != nil {
		return application.LeaseSnapshot{}, false, fmt.Errorf("read EmulationStation lease: %w", err)
	}
	return value, true, nil
}
