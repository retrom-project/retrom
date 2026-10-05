package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/telemetry"
)

type observedRows struct {
	*sql.Rows
	ctx              context.Context
	tx               *transaction
	connection       *sql.Conn
	stopCancellation func() bool
}

func (rows *observedRows) observe(started time.Time) {
	elapsed := time.Since(started)
	telemetry.RecordTiming(rows.ctx, telemetry.RowsRead, elapsed)
	if rows.tx != nil {
		rows.tx.rowsDuration.Add(int64(elapsed))
	}
}

func (rows *observedRows) Next() bool {
	started := time.Now()
	next := rows.Rows.Next()
	rows.observe(started)
	if !next {
		_ = rows.Close()
	}
	return next
}

func (rows *observedRows) Scan(destinations ...any) error {
	defer rows.observe(time.Now())
	if err := rows.Rows.Scan(destinations...); err != nil {
		return fmt.Errorf("scan postgres rows: %w", err)
	}
	return nil
}

func (rows *observedRows) Close() error {
	defer rows.observe(time.Now())
	if rows.stopCancellation != nil {
		rows.stopCancellation()
	}
	err := rows.Rows.Close()
	if rows.connection != nil {
		closeErr := rows.connection.Close()
		rows.connection = nil
		if err == nil && !errors.Is(closeErr, sql.ErrConnDone) {
			err = closeErr
		}
	}
	if err != nil {
		return fmt.Errorf("close postgres rows: %w", err)
	}
	return nil
}
