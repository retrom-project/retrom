package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/service/metadatascrape"
)

func (records workerRecords) Claim(ctx context.Context, claim metadatascrape.WorkerClaim) (bool, error) {
	if claim.Terminal {
		return records.claimTerminal(ctx, claim)
	}
	changed, err := workerChanged(records.transaction.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',
 attempt_count=attempt_count+1,execution_started_at_ms=COALESCE(execution_started_at_ms,?),
 execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,
 worker_id=?,version=version+1,updated_at_ms=?
 WHERE id=? AND execution_no=? AND version=? AND kind='METADATA_SCRAPE'
 AND state='QUEUED' AND available_at_ms<=?
 AND attempt_count<max_attempts AND (execution_deadline_at_ms IS NULL OR execution_deadline_at_ms>?)
 AND EXISTS(SELECT 1 FROM metadata_scrape_runs r WHERE r.id=? AND r.job_id=jobs.id AND r.state='RUNNING')`,
		claim.Now, claim.Deadline, claim.Now+60000, claim.Now, claim.WorkerID, claim.Now,
		claim.JobID, claim.ExecutionNo, claim.Version, claim.Now, claim.Now, claim.RunID))
	if err != nil || !changed {
		return changed, err
	}
	if err := records.event(ctx, claim.JobID, "STARTED", "{}", claim.Now); err != nil {
		return false, err
	}
	return true, nil
}

func (records workerRecords) claimTerminal(ctx context.Context, claim metadatascrape.WorkerClaim) (bool, error) {
	return workerChanged(records.transaction.ExecContext(ctx, `
 UPDATE jobs SET worker_id=?,version=version+1,updated_at_ms=?
 WHERE id=? AND execution_no=? AND version=? AND kind='METADATA_SCRAPE'
 AND (state='QUEUED' OR (state IN ('RUNNING','CANCEL_REQUESTED')
 AND (leased_until_ms<=? OR execution_deadline_at_ms<=?)))
 AND (execution_deadline_at_ms<=? OR attempt_count>=max_attempts OR state='CANCEL_REQUESTED')
 AND EXISTS(SELECT 1 FROM metadata_scrape_runs r WHERE r.id=? AND r.job_id=jobs.id AND r.state='RUNNING')`,
		claim.WorkerID, claim.Now, claim.JobID, claim.ExecutionNo, claim.Version,
		claim.Now, claim.Now, claim.Now, claim.RunID))
}

func (records workerRecords) Refresh(ctx context.Context, claim metadatascrape.WorkerClaim, now int64) (bool, error) {
	return workerChanged(
		records.transaction.ExecContext(
			ctx,
			`UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,updated_at_ms=?
 WHERE id=? AND execution_no=? AND worker_id=? AND state='RUNNING'
 AND leased_until_ms>? AND execution_deadline_at_ms>?`,

			now+60000,
			now,
			now,
			claim.JobID,
			claim.ExecutionNo,
			claim.WorkerID,
			now,
			now,
		),
	)
}

func (records workerRecords) Status(
	ctx context.Context,
	claim metadatascrape.WorkerClaim,
	now int64,
) (metadatascrape.WorkerStatus, error) {
	var status metadatascrape.WorkerStatus
	err := records.transaction.QueryRowContext(
		ctx,
		`SELECT j.state,COALESCE(j.leased_until_ms<=? OR j.execution_deadline_at_ms<=?,1),
 EXISTS(SELECT 1 FROM import_items i JOIN import_jobs p ON p.id=i.import_job_id
 WHERE i.id=r.import_item_id AND p.cancel_requested_at_ms IS NOT NULL)
 FROM jobs j JOIN metadata_scrape_runs r ON r.job_id=j.id WHERE j.id=? AND r.id=? AND r.state='RUNNING'
 AND j.execution_no=? AND (j.worker_id=? OR (?='' AND j.state='CANCELLED'))`,
		now,
		now,
		claim.JobID,
		claim.RunID,
		claim.ExecutionNo,
		claim.WorkerID,
		claim.WorkerID,
	).
		Scan(&status.State, &status.Expired, &status.ParentCancelled)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("query metadata ownership: %w", err)
	}
	return status, nil
}
