package recordstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	dbapi "retrom/internal/database"
)

// Table and SQL fragments are fixed identifiers supplied by domain repositories.
func InsertRows(ctx context.Context, db dbapi.Executor, table, query string, args ...any) (sql.Result, error) {
	if !validTable(table) {
		return nil, ErrInvariant
	}
	return create(ctx, db, query, args, table, "1", nil)
}

func UpdateRows(ctx context.Context, db dbapi.Executor, table string, change Update) (sql.Result, error) {
	if !validTable(table) || change.Scope.Where == "" || change.Set == "" {
		return nil, ErrInvariant
	}
	return Atomic(ctx, db, func(tx dbapi.Executor) (sql.Result, error) {
		args := append(append([]any{}, change.Values...), change.Scope.Args...)
		result, err := tx.ExecContext(ctx, "UPDATE "+table+" SET "+change.Set+" WHERE "+change.Scope.Where, args...)
		if err != nil {
			return nil, fmt.Errorf("update records: %w", err)
		}
		return result, nil
	})
}

func DeleteRows(ctx context.Context, db dbapi.Executor, table string, scope Scope) (sql.Result, error) {
	if !validTable(table) {
		return nil, ErrInvariant
	}
	return deleteRows(ctx, db, table, scope)
}

func upsertRecords(ctx context.Context, db dbapi.Executor, table string,
	query string, args []any, columns string, check validator,
) (sql.Result, error) {
	if !validTable(table) {
		return nil, ErrInvariant
	}
	return Atomic(ctx, db, func(tx dbapi.Executor) (sql.Result, error) {
		keys, err := insertedKeys(ctx, tx, query, args, columns)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			if err := check(ctx, tx, key...); err != nil {
				return nil, err
			}
		}
		return driver.RowsAffected(len(keys)), nil
	})
}

func validTable(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r != '_' && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}
