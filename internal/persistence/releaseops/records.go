package releaseops

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

type (
	Records      struct{ Executor dbapi.Executor }
	RecordUpdate func(context.Context, dbapi.Executor, recordstore.Update) (sql.Result, error)
)

func (records Records) CheckedUpdate(
	ctx context.Context,
	table string,
	write RecordUpdate,
	update recordstore.Update,
) error {
	update.Scope.Where = "rowid IN (SELECT rowid FROM " + table + " WHERE " +
		update.Scope.Where + " ORDER BY rowid LIMIT 200)"
	count, err := records.ReadCount(ctx, "SELECT count(*) FROM "+table+" WHERE "+update.Scope.Where, update.Scope.Args...)
	if err != nil {
		return err
	}
	result, err := write(ctx, records.Executor, update)
	if err := Count(result, err, count); err != nil {
		return err
	}
	return nil
}

func (records Records) ExecUpdate(
	ctx context.Context,
	table, where string,
	countArgs []any,
	query string,
	args ...any,
) error {
	count, err := records.ReadCount(ctx, "SELECT count(*) FROM "+table+" WHERE "+where, countArgs...)
	if err != nil {
		return err
	}
	result, err := records.Executor.ExecContext(ctx, query, args...)
	if err := Count(result, err, count); err != nil {
		return err
	}
	return nil
}

type DeletionBatch struct {
	Table, Where string
	Remove       func(context.Context, dbapi.Executor, recordstore.Scope) (sql.Result, error)
}

// RemoveBatches changes at most one bounded page. The caller commits before continuing.
func (records Records) RemoveBatches(ctx context.Context, batches []DeletionBatch, id string) error {
	for _, batch := range batches {
		batch.Where = "rowid IN (SELECT rowid FROM " + batch.Table + " WHERE " + batch.Where + " ORDER BY rowid LIMIT 200)"
		count, err := records.ReadCount(ctx, "SELECT count(*) FROM "+batch.Table+" WHERE "+batch.Where, id)
		if err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		result, err := batch.Remove(ctx, records.Executor, recordstore.Scope{Where: batch.Where, Args: []any{id}})
		if err := Count(result, err, count); err != nil {
			return fmt.Errorf("delete payload reference batch: %w", err)
		}
		return nil
	}
	return nil
}

func (records Records) ReadCount(ctx context.Context, query string, args ...any) (int64, error) {
	var count int64
	if err := dbapi.QueryRowContext(ctx, records.Executor, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("read release reference count: %w", err)
	}
	return count, nil
}

func Count(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write release reference: %w", err)
	}
	actual, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count release reference changes: %w", err)
	}
	if actual != expected {
		return application.ErrEffectConflict
	}
	return nil
}
