// Package database defines the SQL capabilities used by persistence code.
// Concrete database/sql handles remain inside the SQLite adapter.
package database

import (
	"context"
	"database/sql"
)

// Queryer is the common query capability of a pool or transaction.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
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
type DB interface { //nolint:interfacebloat // Pool, transaction, and lifecycle capabilities are one resource contract.
	Executor
	BeginTx(context.Context, *TxOptions) (Tx, error)
	BeginImmediate(context.Context) (Tx, error)
	PingContext(context.Context) error
	SetMaxOpenConns(int)
	Stats() Stats
	Close() error
}

// Stats contains the pool counters needed by lifecycle and leak checks.
type Stats struct {
	MaxOpenConnections int
	InUse              int
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

type IsolationLevel uint8

const (
	LevelDefault IsolationLevel = iota
	LevelSerializable
)

type TxOptions struct {
	Isolation IsolationLevel
	ReadOnly  bool
}

// Scanner is shared by single-row and multi-row scan helpers.
type Scanner interface{ Scan(...any) error }
