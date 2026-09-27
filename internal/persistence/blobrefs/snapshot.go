package blobrefs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
)

type Snapshot struct {
	Keys   []any
	Counts Delta
}

func Tracked(table string) bool { return columns[table] != "" }

// Capture reads only the rows selected by a domain mutation, before it changes
// their state. Keys retain that exact selection when the predicate changes.
func Capture(ctx context.Context, tx dbapi.Executor, table, where string, args ...any) (Snapshot, error) {
	result := Snapshot{Counts: make(Delta)}
	if !Tracked(table) {
		return result, nil
	}
	if where == "" {
		return result, ErrCount
	}
	rows, err := tx.QueryContext(ctx, "SELECT rowid,"+columns[table]+" FROM "+table+" WHERE "+where, args...)
	if err != nil {
		return result, fmt.Errorf("read %s references: %w", table, err)
	}
	defer func() { cleanup.Error("close Blob references", rows.Close()) }()
	names, err := rows.Columns()
	if err != nil {
		return result, fmt.Errorf("read reference columns: %w", err)
	}
	for rows.Next() {
		var key int64
		values := make([]sql.NullString, len(names)-1)
		pointers := []any{&key}
		for index := range values {
			pointers = append(pointers, &values[index])
		}
		if err := rows.Scan(pointers...); err != nil {
			return result, fmt.Errorf("decode reference: %w", err)
		}
		result.Keys = append(result.Keys, key)
		for _, value := range values {
			if value.Valid {
				if err := result.Counts.Add(value.String, 1); err != nil {
					return result, err
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate references: %w", err)
	}
	return result, nil
}

func CaptureKeys(ctx context.Context, tx dbapi.Executor, table string, keys []any) (Snapshot, error) {
	result := Snapshot{Counts: make(Delta)}
	for start := 0; start < len(keys); start += 200 {
		batch := keys[start:min(start+200, len(keys))]
		where := "rowid IN (" + strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",") + ")"
		part, err := Capture(ctx, tx, table, where, batch...)
		if err != nil {
			return result, err
		}
		result.Keys = append(result.Keys, part.Keys...)
		for id, count := range part.Counts {
			if err := result.Counts.Add(id, count); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func Difference(before, after Snapshot) (Delta, error) {
	delta := make(Delta)
	for id, count := range before.Counts {
		if err := delta.Add(id, -count); err != nil {
			return nil, err
		}
	}
	for id, count := range after.Counts {
		if err := delta.Add(id, count); err != nil {
			return nil, err
		}
	}
	return delta, nil
}
