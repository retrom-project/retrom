package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/service/jobs"
)

func (repository *Repository) WithRead(ctx context.Context, work func(jobs.ReadRecords) error) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin job read snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(records{executor: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit job read snapshot: %w", err)
	}
	return nil
}

func (store records) Detail(ctx context.Context, id string) (jobs.Snapshot, error) {
	var snapshot jobs.Snapshot
	var errorCode sql.NullString
	var retryable sql.NullInt64
	err := store.executor.QueryRowContext(ctx, `
SELECT id,scope_type,scope_id,kind,state,version,attempt_count,max_attempts,error_code,error_retryable,updated_at_ms
FROM jobs WHERE id=?`, id).Scan(&snapshot.JobID, &snapshot.ScopeType, &snapshot.ScopeID, &snapshot.Kind,
		&snapshot.State, &snapshot.Version, &snapshot.AttemptCount, &snapshot.MaxAttempts,
		&errorCode, &retryable, &snapshot.UpdatedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Snapshot{}, jobs.ErrNotFound
	}
	if err != nil {
		return jobs.Snapshot{}, fmt.Errorf("query job detail: %w", err)
	}
	snapshot.ErrorCode = dbexec.StringPointer(errorCode)
	snapshot.Retryable = retryable.Valid && retryable.Int64 == 1
	return snapshot, nil
}

func (store records) ImportProgress(ctx context.Context, id string) (jobs.ImportProgress, error) {
	snapshot := jobs.ImportProgress{ImportJobID: id}
	err := store.executor.QueryRowContext(ctx, `
SELECT state,version,total_item_count,queued_item_count,running_item_count,review_pending_item_count,failed_item_count
FROM import_jobs WHERE id=?`, id).Scan(&snapshot.State, &snapshot.Version, &snapshot.TotalItemCount,
		&snapshot.QueuedItemCount, &snapshot.RunningItemCount, &snapshot.ReviewPendingItemCount, &snapshot.FailedItemCount)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.ImportProgress{}, jobs.ErrNotFound
	}
	if err != nil {
		return jobs.ImportProgress{}, fmt.Errorf("query import progress: %w", err)
	}
	return snapshot, nil
}

func (store records) EventMaximum(ctx context.Context) (int64, error) {
	var maximum int64
	if err := store.executor.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM job_events`).
		Scan(&maximum); err != nil {
		return 0, fmt.Errorf("query event high water mark: %w", err)
	}
	return maximum, nil
}

func (store records) JobEvents(ctx context.Context, query jobs.EventQuery) ([]jobs.Event, error) {
	rows, err := store.executor.QueryContext(ctx, `
SELECT id,event_type,data_json FROM job_events WHERE job_id=? AND id>? ORDER BY id LIMIT ?`,
		query.ResourceID, query.After, query.Limit)
	return scanEvents(rows, err)
}

func (store records) ImportEvents(ctx context.Context, query jobs.EventQuery) ([]jobs.Event, error) {
	rows, err := store.executor.QueryContext(ctx, `
SELECT e.id,e.event_type,e.data_json FROM job_events e
WHERE e.id>? AND ((e.scope_type='IMPORT_GROUP' AND e.scope_id=?) OR
(e.scope_type='IMPORT_ITEM' AND EXISTS(SELECT 1 FROM import_items item
WHERE item.id=e.scope_id AND item.import_job_id=?))) ORDER BY e.id LIMIT ?`,
		query.After, query.ResourceID, query.ResourceID, query.Limit)
	return scanEvents(rows, err)
}

func scanEvents(rows *sql.Rows, err error) ([]jobs.Event, error) {
	if err != nil {
		return nil, fmt.Errorf("query job events: %w", err)
	}
	defer func() { cleanup.Error("close job events", rows.Close()) }()
	items := make([]jobs.Event, 0)
	for rows.Next() {
		var item jobs.Event
		if err := rows.Scan(&item.ID, &item.Type, &item.Data); err != nil {
			return nil, fmt.Errorf("scan job event: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job events: %w", err)
	}
	return items, nil
}
