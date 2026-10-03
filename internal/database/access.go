package database

import (
	"context"
	"fmt"
)

type access struct {
	DB
	reader DB
}

// NewAccess routes standalone reads and explicit read snapshots to the reader.
// Write transactions retain one executor for all of their reads and writes.
// Pool ownership and lifecycle remain with the store that opened both handles.
func NewAccess(reader, writer DB) DB {
	return &access{DB: writer, reader: reader}
}

func (db *access) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := db.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reader: %w", err)
	}
	return rows, nil
}

func (db *access) BeginTx(ctx context.Context, options *TxOptions) (Tx, error) {
	selected := db.DB
	if options != nil && options.ReadOnly {
		selected = db.reader
	}
	tx, err := selected.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin database access transaction: %w", err)
	}
	return tx, nil
}
