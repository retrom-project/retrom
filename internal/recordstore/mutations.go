package recordstore

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

// Scope selects the records participating in one atomic mutation. Empty scopes
// are rejected; an intentional table-wide operation uses Where: "1=1".
type Scope struct {
	Where string
	Args  []any
}

// Update separates values from selection parameters so the same scope selects
// the previous snapshots and the rows being changed, without parsing SQL.
type Update struct {
	Set    string
	Values []any
	Scope  Scope
}

func updateRecords(
	ctx context.Context, db DBTX, change Update, table, columns string, rule string,
) (sql.Result, error) {
	if change.Scope.Where == "" || change.Set == "" {
		return nil, fmt.Errorf("%w: missing update scope or assignments", ErrInvariant)
	}
	return Atomic(ctx, db, func(tx DBTX) (sql.Result, error) {
		previous, err := previousRecords(ctx, tx, table, columns, change.Scope)
		if err != nil {
			return nil, err
		}
		args := append(append([]any{}, change.Values...), change.Scope.Args...)
		result, err := tx.ExecContext(ctx, "UPDATE "+table+" SET "+change.Set+" WHERE "+change.Scope.Where, args...)
		if err != nil {
			return nil, fmt.Errorf("update %s: %w", table, err)
		}
		if err := checkPreviousRecords(ctx, tx, previous, rule); err != nil {
			return nil, err
		}
		return result, nil
	})
}

func deleteRecords(
	ctx context.Context, db DBTX, scope Scope, table, columns string, rule string,
) (sql.Result, error) {
	if scope.Where == "" {
		return nil, fmt.Errorf("%w: missing delete scope", ErrInvariant)
	}
	return Atomic(ctx, db, func(tx DBTX) (sql.Result, error) {
		previous, err := previousRecords(ctx, tx, table, columns, scope)
		if err != nil {
			return nil, err
		}
		if err := checkPreviousRecords(ctx, tx, previous, rule); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+scope.Where, scope.Args...)
		if err != nil {
			return nil, fmt.Errorf("delete %s: %w", table, err)
		}
		return result, nil
	})
}

func checkPreviousRecords(ctx context.Context, db DBTX, previous [][]any, rule string) error {
	for _, values := range previous {
		if err := validate(ctx, db, rule, values); err != nil {
			return err
		}
	}
	return nil
}

func previousRecords(ctx context.Context, db DBTX, table, columns string, scope Scope) ([][]any, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" WHERE "+scope.Where, scope.Args...)
	if err != nil {
		return nil, fmt.Errorf("read previous %s: %w", table, err)
	}
	defer func() { cleanup.Error("close previous records", rows.Close()) }()
	return scanRecords(rows)
}

func scanRecords(rows *sql.Rows) ([][]any, error) {
	names, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read snapshot columns: %w", err)
	}
	var records [][]any
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("read previous record: %w", err)
		}
		records = append(records, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate previous records: %w", err)
	}
	return records, nil
}
