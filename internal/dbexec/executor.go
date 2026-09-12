// Package dbexec defines shared SQL execution contracts for persistence infrastructure.
package dbexec

import (
	"context"
	"database/sql"
)

// Executor is implemented by *sql.DB, *sql.Tx and *sql.Conn.
// Business services use repository ports instead of this SQL interface.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var (
	_ Executor = (*sql.DB)(nil)
	_ Executor = (*sql.Tx)(nil)
	_ Executor = (*sql.Conn)(nil)
)
