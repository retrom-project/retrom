// Package sessionstore owns the transactional persistence of session lifecycles.
package sessionstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/recordstore"
)

func CreateLaunch(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return write(ctx, tx, query, args, createLaunchRelations)
}

func CreateSave(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return write(ctx, tx, query, args, createSaveVersion)
}

// Creation and dependent writes share a savepoint. RETURNING selects
// exactly the inserted rows, including multi-row inserts.
// Close the result before applying relations on the same connection.
func write(ctx context.Context, tx *sql.Tx, query string, args []any,
	apply func(context.Context, recordstore.DBTX, string) error,
) (sql.Result, error) {
	result, err := recordstore.Atomic(ctx, tx, func(connection recordstore.DBTX) (sql.Result, error) {
		ids, err := changedIDs(ctx, connection, query, args)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if err := apply(ctx, connection, id); err != nil {
				return nil, err
			}
		}
		return driver.RowsAffected(len(ids)), nil
	})
	if err != nil {
		return nil, fmt.Errorf("create session and relations: %w", err)
	}
	return result, nil
}

func changedIDs(ctx context.Context, tx recordstore.DBTX, query string, args []any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, strings.TrimSuffix(strings.TrimSpace(query), ";")+" RETURNING id", args...)
	if err != nil {
		return nil, fmt.Errorf("write session: %w", err)
	}
	defer func() { cleanup.Error("close changed session rows", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read changed session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate changed sessions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close changed sessions: %w", err)
	}
	return ids, nil
}
