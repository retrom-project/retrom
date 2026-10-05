package datindex

import (
	"context"
	"fmt"
	"strings"

	dbapi "retrom/internal/database"
)

// The widest catalog record has ten columns: 512 rows stay well below
// PostgreSQL's parameter limit and bound memory while avoiding row-by-row I/O.
const catalogBatchRows = 512

type catalogBatch struct {
	executor dbapi.Execer
	insert   string
	tuple    string
	values   []any
	rows     int
}

func (batch *catalogBatch) add(ctx context.Context, values ...any) error {
	if batch.tuple == "" {
		batch.tuple = "(" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")"
		batch.values = make([]any, 0, catalogBatchRows*len(values))
	}
	batch.values = append(batch.values, values...)
	batch.rows++
	if batch.rows == catalogBatchRows {
		return batch.flush(ctx)
	}
	return nil
}

func (batch *catalogBatch) flush(ctx context.Context) error {
	if batch.rows == 0 {
		return nil
	}
	query := batch.insert + " VALUES " + strings.TrimSuffix(strings.Repeat(batch.tuple+",", batch.rows), ",")
	if _, err := batch.executor.ExecContext(ctx, query, batch.values...); err != nil {
		return fmt.Errorf("datindex/replace batch: %w", err)
	}
	clear(batch.values)
	batch.values = batch.values[:0]
	batch.rows = 0
	return nil
}
