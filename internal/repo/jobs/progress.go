package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/jobs"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) LoadDetail(ctx context.Context, id string) (jobs.Snapshot, error) {
	return records{executor: repository.database}.Detail(ctx, id)
}

func (repository *Repository) LoadJobStreamSnapshot(
	ctx context.Context, id string,
) (jobs.Snapshot, int64, error) {
	return readStreamSnapshot(
		ctx, repository.database, id,
		func(r records, ctx context.Context, id string) (jobs.Snapshot, error) {
			return r.Detail(ctx, id)
		},
	)
}

func (repository *Repository) LoadImportStreamSnapshot(
	ctx context.Context, id string,
) (jobs.ImportProgress, int64, error) {
	return readStreamSnapshot(
		ctx, repository.database, id,
		func(r records, ctx context.Context, id string) (jobs.ImportProgress, error) {
			return r.ImportProgress(ctx, id)
		},
	)
}

func readStreamSnapshot[T any](
	ctx context.Context, db *sql.DB, id string,
	load func(records, context.Context, string) (T, error),
) (T, int64, error) {
	var zero T
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, 0, fmt.Errorf("begin job read snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	r := records{executor: tx}
	snapshot, err := load(r, ctx, id)
	if err != nil {
		return zero, 0, err
	}
	maximum, err := r.EventMaximum(ctx)
	if err != nil {
		return zero, 0, err
	}
	if err := tx.Commit(); err != nil {
		return zero, 0, fmt.Errorf("commit job read snapshot: %w", err)
	}
	return snapshot, maximum, nil
}

func (repository *Repository) LoadJobEvents(
	ctx context.Context, id string, after int64,
) ([]jobs.Event, jobs.Snapshot, error) {
	return readEvents(
		ctx, repository.database, id, after,
		func(r records, ctx context.Context, id string) (jobs.Snapshot, error) {
			return r.Detail(ctx, id)
		},
		func(r records, ctx context.Context, q jobs.EventQuery) ([]jobs.Event, error) {
			return r.JobEvents(ctx, q)
		},
	)
}

func (repository *Repository) LoadImportEvents(
	ctx context.Context, id string, after int64,
) ([]jobs.Event, jobs.ImportProgress, error) {
	return readEvents(
		ctx, repository.database, id, after,
		func(r records, ctx context.Context, id string) (jobs.ImportProgress, error) {
			return r.ImportProgress(ctx, id)
		},
		func(r records, ctx context.Context, q jobs.EventQuery) ([]jobs.Event, error) {
			return r.ImportEvents(ctx, q)
		},
	)
}

func readEvents[T any](
	ctx context.Context, db *sql.DB,
	id string, after int64,
	load func(records, context.Context, string) (T, error),
	query func(records, context.Context, jobs.EventQuery) ([]jobs.Event, error),
) ([]jobs.Event, T, error) {
	var zero T
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, zero, fmt.Errorf("begin job read snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	r := records{executor: tx}
	snapshot, err := load(r, ctx, id)
	if err != nil {
		return nil, zero, err
	}
	q := jobs.EventQuery{
		ResourceID: id, After: after, Limit: jobs.EventBatchSize,
	}
	events, err := query(r, ctx, q)
	if err != nil {
		return nil, zero, err
	}
	if err := tx.Commit(); err != nil {
		return nil, zero, fmt.Errorf("commit job read snapshot: %w", err)
	}
	return events, snapshot, nil
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
