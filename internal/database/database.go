// Package database defines the SQL capabilities used by persistence code.
// Concrete database/sql handles remain inside the SQLite adapter.
package database

import (
	"context"
	"database/sql"
	"time"
)

// Queryer is the common query capability of a pool or transaction.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (Rows, error)
}

// Rows keeps SQL iteration inside the adapter so connection occupancy and row
// consumption can be observed independently of query submission.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
	Columns() ([]string, error)
}

// Execer is the common write capability of a pool or transaction.
type Execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Executor interface {
	Queryer
	Execer
}

// DB owns a connection pool. Transactions expose only the database contract.
type DB interface {
	Executor
	// BeginTx starts a write transaction unless ReadOnly is explicitly set.
	BeginTx(context.Context, *TxOptions) (Tx, error)
	PingContext(context.Context) error
	SetMaxOpenConns(int)
	Stats() Stats
	Close() error
}

// Stats contains pool counters and SQL/transaction observations.
type Stats struct {
	MaxOpenConnections  int
	InUse               int
	WaitCount           int64
	WaitDuration        time.Duration
	SQLCalls            int64
	SQLCallDuration     time.Duration
	Transactions        int64
	TransactionDuration time.Duration
}

type Tx interface {
	Executor
	// NowMS uses the database's injected clock for transactional bookkeeping.
	NowMS() int64
	PrepareContext(context.Context, string) (Stmt, error)
	Commit() error
	Rollback() error
}

type Stmt interface {
	ExecContext(context.Context, ...any) (sql.Result, error)
	Close() error
}

type TxOptions struct {
	ReadOnly bool
}

// Scanner is shared by single-row and multi-row scan helpers.
type Scanner interface{ Scan(...any) error }
