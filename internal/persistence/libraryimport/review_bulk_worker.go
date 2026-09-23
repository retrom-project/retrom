package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
)

var ErrReviewBulkNotRunnable = errors.New("review bulk worker no longer owns task")

type ReviewBulkScanItem struct {
	ID                string
	ReviewVersion     int64
	ReviewUpdatedAtMS int64
	ItemUpdatedAtMS   int64
	CreatedAtMS       int64
}

type ReviewBulkWorker struct{ executor dbexec.Executor }

func BindReviewBulkWorker(executor dbexec.Executor) *ReviewBulkWorker {
	return &ReviewBulkWorker{executor: executor}
}

func (worker *ReviewBulkWorker) Claim(ctx context.Context, bulkID, workerID string, now int64) (string, string, error) {
	var jobID, userID string
	err := worker.executor.QueryRowContext(ctx, `
SELECT bulk.job_id,bulk.created_by_user_id FROM review_bulk_approvals bulk
JOIN jobs job ON job.id=bulk.job_id
WHERE bulk.id=? AND bulk.state='QUEUED' AND job.state='QUEUED'`, bulkID).Scan(&jobID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrReviewBulkNotRunnable
	}
	if err != nil {
		return "", "", fmt.Errorf("read bulk claim: %w", err)
	}
	result, err := worker.executor.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND state='QUEUED'
`, workerID, now, now+3_600_000, now+60_000, now, now, jobID)
	if err := bulkMutation(result, err, "claim job"); err != nil {
		return "", "", err
	}
	result, err = recordstore.UpdateReviewBulkApprovals(ctx, worker.executor, recordstore.Update{
		Set:    `state='RUNNING',started_at_ms=COALESCE(started_at_ms,?),version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='QUEUED'`, Args: []any{bulkID}},
		Values: []any{now, now},
	})
	if err := bulkMutation(result, err, "claim bulk"); err != nil {
		return "", "", err
	}
	return jobID, userID, nil
}

func (worker *ReviewBulkWorker) Next(ctx context.Context, bulkID, workerID string) (ReviewBulkScanItem, bool, error) {
	var item ReviewBulkScanItem
	err := worker.executor.QueryRowContext(ctx, `
SELECT item.id,item.review_version,item.review_updated_at_ms,item.updated_at_ms,bulk.created_at_ms
FROM review_bulk_approvals bulk
JOIN jobs job ON job.id=bulk.job_id
JOIN import_items item ON item.id<=bulk.max_item_id
WHERE bulk.id=? AND bulk.state='RUNNING' AND job.state='RUNNING' AND job.worker_id=?
AND item.state='REVIEW_PENDING' AND item.review_version>0
AND item.created_at_ms<=bulk.created_at_ms
AND (bulk.cursor_item_id IS NULL OR item.id>bulk.cursor_item_id)
ORDER BY item.id LIMIT 1`, bulkID, workerID).Scan(
		&item.ID, &item.ReviewVersion, &item.ReviewUpdatedAtMS, &item.ItemUpdatedAtMS, &item.CreatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewBulkScanItem{}, false, nil
	}
	if err != nil {
		return ReviewBulkScanItem{}, false, fmt.Errorf("read next review bulk item: %w", err)
	}
	return item, true, nil
}

func (worker *ReviewBulkWorker) Skip(
	ctx context.Context, bulkID, jobID, workerID, itemID, outcome string, now int64,
) error {
	if outcome != "CHANGED" && outcome != "DUPLICATE" && outcome != "NOT_READY" {
		return ErrReviewBulkNotRunnable
	}
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, worker.executor, recordstore.Update{
		Set: `cursor_item_id=?,scanned_count=scanned_count+1,
skipped_changed_count=skipped_changed_count+CASE WHEN ?='CHANGED' THEN 1 ELSE 0 END,
skipped_duplicate_count=skipped_duplicate_count+CASE WHEN ?='DUPLICATE' THEN 1 ELSE 0 END,
skipped_not_ready_count=skipped_not_ready_count+CASE WHEN ?='NOT_READY' THEN 1 ELSE 0 END,
version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND job_id=? AND state='RUNNING' AND max_item_id>=?
AND (cursor_item_id IS NULL OR cursor_item_id<?)
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?)`,
			Args: []any{bulkID, jobID, itemID, itemID, jobID, workerID},
		},
		Values: []any{itemID, outcome, outcome, outcome, now},
	})
	if err := bulkMutation(result, err, "skip bulk item"); err != nil {
		return err
	}
	return worker.Heartbeat(ctx, jobID, workerID, now)
}

func (worker *ReviewBulkWorker) Heartbeat(ctx context.Context, jobID, workerID string, now int64) error {
	result, err := worker.executor.ExecContext(ctx, `
UPDATE jobs SET heartbeat_at_ms=?,leased_until_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?`, now, now+60_000, now, jobID, workerID)
	return bulkMutation(result, err, "heartbeat bulk job")
}

func (worker *ReviewBulkWorker) Finish(ctx context.Context, bulkID, jobID, workerID string, now int64) error {
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, worker.executor, recordstore.Update{
		Set: `state='COMPLETED',completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND job_id=? AND state='RUNNING'
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='RUNNING' AND worker_id=?)`,
			Args: []any{bulkID, jobID, jobID, workerID},
		},
		Values: []any{now, now},
	})
	if err := bulkMutation(result, err, "finish bulk"); err != nil {
		return err
	}
	result, err = worker.executor.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,worker_id=NULL,
heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?`, now, now, now, jobID, workerID)
	return bulkMutation(result, err, "finish bulk job")
}

func (worker *ReviewBulkWorker) Fail(ctx context.Context, bulkID, jobID, workerID string, now int64) error {
	result, err := recordstore.UpdateReviewBulkApprovals(ctx, worker.executor, recordstore.Update{
		Set: `state='FAILED',last_error_code='REVIEW_BULK_WORKER_UNAVAILABLE',
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND job_id=? AND state='RUNNING'`,
			Args:  []any{bulkID, jobID},
		},
		Values: []any{now, now},
	})
	if err := bulkMutation(result, err, "fail bulk"); err != nil {
		return err
	}
	result, err = worker.executor.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='REVIEW_BULK_WORKER_UNAVAILABLE',
error_retryable=1,finished_at_ms=?,leased_until_ms=NULL,worker_id=NULL,
version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?`, now, now, jobID, workerID)
	return bulkMutation(result, err, "fail bulk job")
}

func (worker *ReviewBulkWorker) Resume(ctx context.Context, now int64) ([]string, error) {
	if _, err := worker.executor.ExecContext(ctx, `
UPDATE jobs SET state='QUEUED',worker_id=NULL,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
available_at_ms=?,version=version+1,updated_at_ms=?
WHERE kind='REVIEW_BULK_APPROVE' AND state='RUNNING'`, now, now); err != nil {
		return nil, fmt.Errorf("queue interrupted bulk jobs: %w", err)
	}
	if _, err := recordstore.UpdateReviewBulkApprovals(ctx, worker.executor, recordstore.Update{
		Set:    `state='QUEUED',version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `state='RUNNING'`},
		Values: []any{now},
	}); err != nil {
		return nil, fmt.Errorf("queue interrupted bulk approvals: %w", err)
	}
	rows, err := worker.executor.QueryContext(ctx, `
SELECT id FROM review_bulk_approvals WHERE state='QUEUED' ORDER BY created_at_ms,id`)
	if err != nil {
		return nil, fmt.Errorf("list resumable bulk jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan resumable bulk job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list resumable bulk jobs: %w", err)
	}
	return ids, nil
}

func bulkMutation(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows: %w", action, err)
	}
	if changed != 1 {
		return ErrReviewBulkNotRunnable
	}
	return nil
}
