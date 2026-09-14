package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type Leases struct{ database *sql.DB }

func NewLeases(database *sql.DB) *Leases { return &Leases{database: database} }
func (repository *Leases) WithLease(ctx context.Context, work func(application.LeaseRecords) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus lease: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(leaseRecords{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus lease: %w", err)
	}
	return nil
}

type leaseRecords struct{ tx *sql.Tx }

func (records leaseRecords) Next(ctx context.Context, now int64) (application.LeaseCandidate, bool, error) {
	var result application.LeaseCandidate
	err := records.tx.QueryRowContext(ctx, `SELECT job.id,plan.id,job.kind,plan.root_id,plan.root_config_digest,
plan.source_relative_path,plan.created_by_user_id,job.execution_no,job.attempt_count,job.version,plan.version,
plan.state,job.max_attempts,job.execution_started_at_ms,job.execution_deadline_at_ms
FROM jobs job JOIN pegasus_imports plan ON plan.id=job.scope_id
WHERE job.scope_type='PEGASUS_IMPORT' AND job.state='QUEUED' AND job.available_at_ms<=?
AND ((job.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=job.id AND plan.import_job_id IS NULL
AND plan.state='SCANNING')
OR (job.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=job.id AND plan.state='QUEUED'))
AND job.attempt_count<job.max_attempts AND (job.execution_deadline_at_ms IS NULL OR job.execution_deadline_at_ms>?)
ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1`, now, now).Scan(

		&result.Work.JobID,
		&result.Work.ImportID,
		&result.Work.Kind,
		&result.Work.RootID,
		&result.Work.RootDigest,

		&result.Work.RelativePath,
		&result.Work.CreatedByUserID,
		&result.Work.ExecutionNo,
		&result.Work.Attempt,

		&result.JobVersion,
		&result.ImportVersion,
		&result.ImportState,
		&result.MaxAttempts,
		&result.StartedAtMS,
		&result.DeadlineAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.LeaseCandidate{}, false, nil
	}
	if err != nil {
		return application.LeaseCandidate{}, false, fmt.Errorf("read queued Pegasus execution: %w", err)
	}
	return result, true, nil
}

func (records leaseRecords) Claim(ctx context.Context, change application.LeaseClaim) error {
	before, unit := change.Before, change.Work
	result, err := records.tx.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',attempt_count=?,
execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,worker_id=?,
version=version+1,updated_at_ms=? WHERE id=? AND version=? AND state='QUEUED' AND execution_no=?
AND attempt_count=? AND attempt_count<max_attempts AND available_at_ms<=?
AND execution_started_at_ms IS ? AND execution_deadline_at_ms IS ?
AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=jobs.id)))`,
		unit.Attempt, change.StartedAtMS, unit.DeadlineAtMS, change.LeaseUntilMS, change.NowMS, unit.WorkerID, change.NowMS,
		unit.JobID, before.JobVersion, unit.ExecutionNo, before.Work.Attempt, change.NowMS,
		before.StartedAtMS, before.DeadlineAtMS,
		unit.ImportID, before.ImportVersion, before.ImportState)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = recordstore.UpdatePegasusImports(ctx, records.tx, recordstore.Update{
		Set: `state=?,phase=?,started_at_ms=CASE WHEN ?='SERVER_PEGASUS_IMPORT' THEN COALESCE(started_at_ms,?)
ELSE started_at_ms END,version=version+1,updated_at_ms=?`,
		Values: []any{change.ImportState, change.Phase, unit.Kind, change.NowMS, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{unit.ImportID, before.ImportVersion, before.ImportState},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	event, err := json.Marshal(struct {
		SchemaVersion int   `json:"schemaVersion"`
		ExecutionNo   int64 `json:"executionNo"`
		Attempt       int64 `json:"attempt"`
	}{1, unit.ExecutionNo, unit.Attempt})
	if err != nil {
		return fmt.Errorf("encode Pegasus claim: %w", err)
	}
	_, err = records.tx.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'STARTED',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(event),
		change.NowMS,
	)
	if err != nil {
		return fmt.Errorf("record Pegasus claim: %w", err)
	}
	return nil
}

func (records leaseRecords) Current(ctx context.Context, id string) (application.ExecutionSnapshot, error) {
	return scanRecovery(records.tx.QueryRowContext(ctx, recoverySnapshotSQL+` AND job.id=?`, id))
}

func (records leaseRecords) Renew(ctx context.Context, change application.LeaseRenewal) error {
	before := change.Before
	result, err := records.tx.ExecContext(
		ctx,
		`UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,
version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state=? AND execution_no=? AND attempt_count=? AND worker_id=?
AND leased_until_ms=? AND leased_until_ms>? AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND ((jobs.kind='SERVER_PEGASUS_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_PEGASUS_IMPORT' AND plan.import_job_id=jobs.id)))`,

		change.NowMS,
		change.LeaseUntilMS,
		change.NowMS,
		before.JobID,
		before.JobVersion,
		before.JobState,

		before.ExecutionNo,
		before.Attempt,
		before.WorkerID,
		before.LeaseUntilMS,
		change.NowMS,
		before.DeadlineMS,
		change.NowMS,

		before.ImportID,
		before.ImportVersion,
		before.ImportState,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
