package gamecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/service/gamecontent"
)

func (writes writes) Claim(ctx context.Context, claim gamecontent.Claim) (bool, error) {
	claimed, err := changed(
		writes.transaction.ExecContext(
			ctx,
			`UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,
 worker_id=?,version=version+1,updated_at_ms=?
 WHERE id=? AND scope_type='GAME' AND scope_id=? AND kind='GAME_CONTENT_REPLACE'
 AND state='QUEUED' AND execution_no=? AND available_at_ms<=?
 AND attempt_count<max_attempts AND EXISTS(SELECT 1 FROM job_input_snapshots input
 WHERE input.job_id=jobs.id AND input.execution_no=jobs.execution_no AND input.input_digest=?)`,

			claim.Now,
			claim.Deadline,
			claim.Now+60_000,
			claim.Now,
			claim.WorkerID,
			claim.Now,
			claim.JobID,
			claim.GameID,
			claim.ExecutionNo,
			claim.Now,
			claim.InputDigest,
		),
	)
	if err != nil || !claimed {
		return claimed, err
	}
	if err := writes.event(ctx, claim.JobID, "STARTED", "{}", claim.Now); err != nil {
		return false, err
	}
	return true, nil
}

func (writes writes) Refresh(ctx context.Context, claim gamecontent.Claim, now int64) (bool, error) {
	return changed(writes.transaction.ExecContext(ctx, `UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,updated_at_ms=?
 WHERE id=? AND execution_no=? AND worker_id=? AND state='RUNNING'
 AND leased_until_ms>? AND execution_deadline_at_ms>?`,
		now+60_000, now, now, claim.JobID, claim.ExecutionNo, claim.WorkerID, now, now))
}

func (writes writes) Current(ctx context.Context, claim gamecontent.Claim, now int64) (bool, error) {
	var current bool
	err := writes.transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs
 WHERE id=? AND execution_no=? AND worker_id=? AND state='RUNNING'
 AND leased_until_ms>? AND execution_deadline_at_ms>?)`,
		claim.JobID, claim.ExecutionNo, claim.WorkerID, now, now).Scan(&current)
	if err != nil {
		return false, fmt.Errorf("read replacement lease: %w", err)
	}
	return current, nil
}

func (writes writes) State(ctx context.Context, claim gamecontent.Claim) (string, error) {
	var state string
	err := writes.transaction.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND execution_no=? AND worker_id=?`,
		claim.JobID, claim.ExecutionNo, claim.WorkerID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read replacement owner state: %w", err)
	}
	return state, nil
}
