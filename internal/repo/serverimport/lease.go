package serverimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/model/serverimport"
	"retrom/internal/repo/dbexec"

	"github.com/google/uuid"
)

type Leases struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewLeases(database *sql.DB) *Leases { return &Leases{database: database} }

func (l *Leases) WithPreCommitHook(hook func() error) *Leases {
	l.preCommitHook = hook
	return l
}

func (repository *Leases) CommitClaim(
	ctx context.Context, cmd serverimport.ClaimCommand,
) (serverimport.ClaimResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return serverimport.ClaimResult{},
			fmt.Errorf("begin import lease: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := leaseRecords{tx}
	before, ok, err := records.Next(ctx, cmd.Now)
	if err != nil {
		return serverimport.ClaimResult{},
			fmt.Errorf("read claimable import: %w", err)
	}
	if !ok {
		return serverimport.ClaimResult{}, nil
	}
	if before.State != before.ImportState ||
		before.Attempt >= before.Maximum {
		return serverimport.ClaimResult{}, serverimport.ErrLeaseLost
	}

	owner, err := uuid.NewV7()
	if err != nil {
		return serverimport.ClaimResult{},
			fmt.Errorf("create import worker identity: %w", err)
	}

	unit := before.Work
	unit.Owner = owner.String()
	unit.DeadlineAtMS = cmd.Now + (8 * time.Hour).Milliseconds()
	if before.Deadline != nil {
		unit.DeadlineAtMS = *before.Deadline
	}

	plan := serverimport.LeaseClaim{
		Before:     before,
		Work:       unit,
		Now:        cmd.Now,
		LeaseUntil: cmd.Now + time.Minute.Milliseconds(),
		Event:      []byte(`{"schemaVersion":1,"phase":"PREPARING_ROOT"}`),
	}
	if before.State == "RUNNING" {
		plan.RecoveryEvent = []byte(
			`{"schemaVersion":1,"reason":"LEASE_EXPIRED"}`,
		)
	}

	if err := records.Claim(ctx, plan); err != nil {
		return serverimport.ClaimResult{},
			fmt.Errorf("claim import execution: %w", err)
	}

	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return serverimport.ClaimResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return serverimport.ClaimResult{},
			fmt.Errorf("commit import lease: %w", err)
	}
	return serverimport.ClaimResult{Unit: unit, Found: true}, nil
}

func (repository *Leases) CommitTouch(
	ctx context.Context, cmd serverimport.TouchCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import lease: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := leaseRecords{tx}
	before, err := records.Current(ctx, cmd.Unit.JobID)
	if err != nil {
		return fmt.Errorf("read import lease: %w", err)
	}

	if err := validateTouch(before, cmd); err != nil {
		return err
	}

	if err := records.Touch(ctx, serverimport.LeaseTouch{
		Before:     before,
		Now:        cmd.Now,
		LeaseUntil: cmd.Now + time.Minute.Milliseconds(),
		Phase:      cmd.Phase,
		Event:      cmd.Event,
	}); err != nil {
		return fmt.Errorf("renew import lease: %w", err)
	}

	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import lease: %w", err)
	}
	return nil
}

func validateTouch(
	before serverimport.LeaseSnapshot, cmd serverimport.TouchCommand,
) error {
	if before.Work.ImportID != cmd.Unit.ImportID ||
		before.Work.Execution != cmd.Unit.Execution ||
		before.Work.Owner != cmd.Unit.Owner ||
		cmd.Unit.Owner == "" {
		return serverimport.ErrLeaseLost
	}
	if before.State == "CANCEL_REQUESTED" ||
		before.State == "CANCELLED" {
		return serverimport.ErrWorkerCancelled
	}
	if before.State != "RUNNING" ||
		before.ImportState != "RUNNING" ||
		before.LeaseUntil == nil ||
		*before.LeaseUntil <= cmd.Now {
		return serverimport.ErrLeaseLost
	}
	return nil
}

type leaseRecords struct{ executor dbexec.Executor }

const leaseProjection = `SELECT import.id,import.job_id,import.root_id,import.source_relative_path,
import.root_config_digest,import.catalog_snapshot_digest,import.replace_if_better,job.execution_no,
COALESCE(job.worker_id,''),job.state,import.state,job.version,import.version,job.attempt_count,
job.max_attempts,job.available_at_ms,job.leased_until_ms,job.execution_deadline_at_ms
FROM server_imports import JOIN jobs job ON job.id=import.job_id `

func (records leaseRecords) Next(
	ctx context.Context, now int64,
) (serverimport.LeaseSnapshot, bool, error) {
	value, err := scanLease(records.executor.QueryRowContext(ctx, leaseProjection+`
WHERE import.state IN ('QUEUED','RUNNING') AND job.attempt_count<job.max_attempts AND (
(job.state='QUEUED' AND job.available_at_ms<=?) OR
(job.state='RUNNING' AND job.leased_until_ms IS NOT NULL AND job.leased_until_ms<=?))
ORDER BY import.created_at_ms,import.id LIMIT 1`, now, now))
	if errors.Is(err, sql.ErrNoRows) {
		return serverimport.LeaseSnapshot{}, false, nil
	}
	if err != nil {
		return serverimport.LeaseSnapshot{}, false, err
	}
	return value, true, nil
}

func (records leaseRecords) Current(
	ctx context.Context, jobID string,
) (serverimport.LeaseSnapshot, error) {
	value, err := scanLease(records.executor.QueryRowContext(
		ctx, leaseProjection+`WHERE job.id=?`, jobID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return serverimport.LeaseSnapshot{}, serverimport.ErrLeaseLost
	}
	return value, err
}

func scanLease(row dbexec.Scanner) (serverimport.LeaseSnapshot, error) {
	var value serverimport.LeaseSnapshot
	err := row.Scan(
		&value.Work.ImportID, &value.Work.JobID, &value.Work.RootID,
		&value.Work.RelativePath,
		&value.Work.RootDigest, &value.Work.CatalogDigest,
		&value.Work.ReplaceIfBetter, &value.Work.Execution,
		&value.Work.Owner, &value.State, &value.ImportState,
		&value.JobVersion, &value.ImportVersion,
		&value.Attempt, &value.Maximum, &value.AvailableAt,
		&value.LeaseUntil, &value.Deadline,
	)
	if err != nil {
		return serverimport.LeaseSnapshot{},
			fmt.Errorf("read import lease snapshot: %w", err)
	}
	return value, nil
}

func (records leaseRecords) Claim(
	ctx context.Context, plan serverimport.LeaseClaim,
) error {
	before := plan.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,
 execution_started_at_ms=COALESCE(execution_started_at_ms,?),
 execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,
 worker_id=?,version=version+1,updated_at_ms=?
 WHERE id=? AND version=? AND execution_no=? AND state=? AND attempt_count<max_attempts AND (
 (state='QUEUED' AND available_at_ms<=?) OR
 (state='RUNNING' AND leased_until_ms IS NOT NULL AND leased_until_ms<=?))`,
		plan.Now, plan.Work.DeadlineAtMS, plan.LeaseUntil, plan.Now,
		plan.Work.Owner, plan.Now,
		plan.Work.JobID, before.JobVersion, plan.Work.Execution,
		before.State, plan.Now, plan.Now)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `UPDATE server_imports SET state='RUNNING',
 phase=CASE WHEN state='QUEUED' THEN 'PREPARING_ROOT' ELSE phase END,version=version+1,updated_at_ms=?
 WHERE id=? AND version=? AND state=?`,
		plan.Now, plan.Work.ImportID, before.ImportVersion, before.ImportState)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	if len(plan.RecoveryEvent) > 0 {
		if err := records.event(
			ctx, plan.Work, "RETRY_SCHEDULED", plan.RecoveryEvent, plan.Now,
		); err != nil {
			return err
		}
	}
	return records.event(ctx, plan.Work, "STARTED", plan.Event, plan.Now)
}

func (records leaseRecords) Touch(
	ctx context.Context, plan serverimport.LeaseTouch,
) error {
	before := plan.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,
 version=version+1,updated_at_ms=? WHERE id=? AND version=? AND execution_no=? AND worker_id=?
 AND state='RUNNING' AND leased_until_ms>?`,
		plan.Now, plan.LeaseUntil, plan.Now,
		before.Work.JobID, before.JobVersion, before.Work.Execution,
		before.Work.Owner, plan.Now)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	if plan.Phase == "" {
		return nil
	}
	result, err = records.executor.ExecContext(ctx, `UPDATE server_imports SET phase=?,version=version+1,
 updated_at_ms=? WHERE id=? AND version=? AND state='RUNNING'`,
		plan.Phase, plan.Now,
		before.Work.ImportID, before.ImportVersion)
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	return records.event(ctx, before.Work, "PROGRESS", plan.Event, plan.Now)
}

func (records leaseRecords) event(
	ctx context.Context,
	unit serverimport.Work,
	event string,
	data []byte,
	now int64,
) error {
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,
 data_json,created_at_ms) VALUES(?,'SERVER_IMPORT',?,?,?,?)`,
		unit.JobID,
		unit.ImportID,
		event,
		string(data),
		now,
	); err != nil {
		return fmt.Errorf("append import worker event: %w", err)
	}
	return nil
}
