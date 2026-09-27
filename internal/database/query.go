package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Row keeps QueryContext's error until Scan, like database/sql.QueryRowContext.
// Scan reads the first row and closes the result set, including on failure.
type Row struct {
	rows *sql.Rows
	err  error
}

var errMissingRowsHandle = errors.New("query returned no rows handle")

func QueryRowContext(ctx context.Context, queryer Queryer, query string, args ...any) *Row {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err == nil && rows != nil {
		if rowsErr := rows.Err(); rowsErr != nil {
			failedRows := rows
			defer func() { _ = failedRows.Close() }()
			err = fmt.Errorf("query rows: %w", rowsErr)
			rows = nil
		}
	}
	return &Row{rows: rows, err: err}
}

// Err reports a query error before Scan, matching database/sql.Row's behavior.
func (row *Row) Err() error { return row.err }

func (row *Row) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if row.rows == nil {
		return errMissingRowsHandle
	}
	if !row.rows.Next() {
		err := row.rows.Err()
		closeErr := row.rows.Close()
		if err != nil {
			return errors.Join(err, closeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close query rows: %w", closeErr)
		}
		return sql.ErrNoRows
	}
	scanErr := row.rows.Scan(destinations...)
	closeErr := row.rows.Close()
	return errors.Join(scanErr, closeErr, row.rows.Err())
}

// ColumnMap exposes the returned column order for dynamic result projections.
func ColumnMap(rows *sql.Rows) (map[string]int, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read query columns: %w", err)
	}
	indices := make(map[string]int, len(columns))
	for index, name := range columns {
		indices[name] = index
	}
	return indices, nil
}
