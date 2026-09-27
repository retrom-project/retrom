package recordstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobrefs"
)

// CreateReferences is used by domain inserts without additional record rules.
// Table is a fixed schema identifier supplied by the domain, never user input.
func CreateReferences(ctx context.Context, db dbapi.Executor, table, query string, args ...any) (sql.Result, error) {
	if !blobrefs.Tracked(table) {
		return nil, ErrInvariant
	}
	return create(ctx, db, query, args, table, "rowid", nil)
}

func UpdateReferences(ctx context.Context, db dbapi.Executor, table string, change Update) (sql.Result, error) {
	if !blobrefs.Tracked(table) || change.Scope.Where == "" || change.Set == "" {
		return nil, ErrInvariant
	}
	return Atomic(ctx, db, func(tx dbapi.Executor) (sql.Result, error) {
		return referenceMutation(ctx, tx, table, change.Scope, false, func() (sql.Result, error) {
			args := append(append([]any{}, change.Values...), change.Scope.Args...)
			return tx.ExecContext(ctx, "UPDATE "+table+" SET "+change.Set+" WHERE "+change.Scope.Where, args...)
		})
	})
}

func DeleteReferences(ctx context.Context, db dbapi.Executor, table string, scope Scope) (sql.Result, error) {
	return deleteRows(ctx, db, table, scope)
}

func referenceMutation(ctx context.Context, tx dbapi.Executor, table string, scope Scope, removing bool,
	write func() (sql.Result, error),
) (sql.Result, error) {
	before, err := blobrefs.Capture(ctx, tx, table, scope.Where, scope.Args...)
	if err != nil {
		return nil, fmt.Errorf("record reference mutation: %w", err)
	}
	result, err := write()
	if err != nil {
		return nil, fmt.Errorf("record reference mutation: %w", err)
	}
	if !blobrefs.Tracked(table) {
		return result, nil
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read reference mutation count: %w", err)
	}
	if count != int64(len(before.Keys)) {
		return nil, fmt.Errorf("%w: %s affected %d expected %d", blobrefs.ErrCount, table, count, len(before.Keys))
	}
	after := blobrefs.Snapshot{}
	if !removing {
		after, err = blobrefs.CaptureKeys(ctx, tx, table, before.Keys)
		if err != nil {
			return nil, fmt.Errorf("record reference mutation: %w", err)
		}
		if len(after.Keys) != len(before.Keys) {
			return nil, blobrefs.ErrCount
		}
	}
	if err := blobrefs.Commit(ctx, tx, before, after); err != nil {
		return nil, fmt.Errorf("record reference mutation: %w", err)
	}
	return result, nil
}

func insertedReferences(ctx context.Context, tx dbapi.Executor, table string, keys [][]any) error {
	if !blobrefs.Tracked(table) {
		return nil
	}
	ids := make([]any, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, key[0])
	}
	after, err := blobrefs.CaptureKeys(ctx, tx, table, ids)
	if err != nil {
		return fmt.Errorf("record reference mutation: %w", err)
	}
	if len(after.Keys) != len(keys) {
		return blobrefs.ErrCount
	}
	if err := blobrefs.Commit(ctx, tx, blobrefs.Snapshot{}, after); err != nil {
		return fmt.Errorf("count inserted references: %w", err)
	}
	return nil
}

func upsertReferences(ctx context.Context, db dbapi.Executor, table string, scope Scope,
	query string, args []any, columns string, check validator,
) (sql.Result, error) {
	return Atomic(ctx, db, func(tx dbapi.Executor) (sql.Result, error) {
		before, err := blobrefs.Capture(ctx, tx, table, scope.Where, scope.Args...)
		if err != nil {
			return nil, fmt.Errorf("record reference mutation: %w", err)
		}
		keys, err := insertedKeys(ctx, tx, query, args, columns)
		if err != nil {
			return nil, fmt.Errorf("record reference mutation: %w", err)
		}
		for _, key := range keys {
			if err := check(ctx, tx, key...); err != nil {
				return nil, fmt.Errorf("record reference mutation: %w", err)
			}
		}
		after, err := blobrefs.Capture(ctx, tx, table, scope.Where, scope.Args...)
		if err != nil {
			return nil, fmt.Errorf("record reference mutation: %w", err)
		}
		if err := blobrefs.Commit(ctx, tx, before, after); err != nil {
			return nil, fmt.Errorf("record reference mutation: %w", err)
		}
		return driver.RowsAffected(len(keys)), nil
	})
}
