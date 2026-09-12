// Package recordstore validates relational ownership at explicit write boundaries.
package recordstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
)

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var (
	ErrInvariant           = errors.New("RECORD_INVARIANT_VIOLATION")
	errTransactionRequired = errors.New("record write requires a database transaction")
)

type validator func(context.Context, DBTX, ...any) error

func create(
	ctx context.Context, db DBTX, query string, args []any, columns string, check validator,
) (sql.Result, error) {
	return Atomic(ctx, db, func(tx DBTX) (sql.Result, error) {
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

func insertedKeys(ctx context.Context, tx DBTX, query string, args []any, columns string) ([][]any, error) {
	rows, err := tx.QueryContext(ctx, strings.TrimSuffix(strings.TrimSpace(query), ";")+" RETURNING "+columns, args...)
	if err != nil {
		return nil, fmt.Errorf("create record: %w", err)
	}
	defer func() { cleanup.Error("close inserted record keys", rows.Close()) }()
	names, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read inserted key columns: %w", err)
	}
	var keys [][]any
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("read inserted key: %w", err)
		}
		keys = append(keys, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inserted keys: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close inserted keys: %w", err)
	}
	return keys, nil
}

func validate(ctx context.Context, db DBTX, query string, args []any) error {
	var message string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&message); err != nil {
		return fmt.Errorf("validate record ownership: %w", err)
	}
	if message != "" {
		return fmt.Errorf("%w: %s", ErrInvariant, message)
	}
	return nil
}
