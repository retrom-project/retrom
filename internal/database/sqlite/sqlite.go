// Package sqlite adapts database/sql's SQLite handles to Retrom's database contract.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // Register SQLite for sql.Open in this adapter.

	"retrom/internal/database"
)

type Options struct {
	MaxOpenConns int
	MaxIdleConns int
}

type (
	handle      struct{ raw *sql.DB }
	transaction struct{ raw *sql.Tx }
)

type immediateTransaction struct {
	connection *sql.Conn
	ctx        context.Context
	done       bool
}

var errUnsupportedIsolation = errors.New("unsupported transaction isolation")

var (
	_ database.DB = (*handle)(nil)
	_ database.Tx = (*transaction)(nil)
	_ database.Tx = (*immediateTransaction)(nil)
)

func Open(dsn string, options Options) (database.DB, error) {
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	configure(raw, options)
	return &handle{raw: raw}, nil
}

// OpenConnector supports driver-boundary fault injection without exposing a SQL pool.
func OpenConnector(connector driver.Connector, options Options) database.DB {
	raw := sql.OpenDB(connector)
	configure(raw, options)
	return &handle{raw: raw}
}

func configure(raw *sql.DB, options Options) {
	if options.MaxOpenConns > 0 {
		raw.SetMaxOpenConns(options.MaxOpenConns)
	}
	if options.MaxIdleConns > 0 {
		raw.SetMaxIdleConns(options.MaxIdleConns)
	}
}

func (db *handle) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := db.raw.ExecContext(ctx, query, args...)
	return wrapResult("execute sqlite query", result, err)
}

func (db *handle) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := db.raw.QueryContext(ctx, query, args...)
	return wrapRows("query sqlite database", rows, err)
}

func (db *handle) PingContext(ctx context.Context) error {
	if err := db.raw.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	return nil
}

func (db *handle) Close() error {
	if err := db.raw.Close(); err != nil {
		return fmt.Errorf("close sqlite database: %w", err)
	}
	return nil
}

func (db *handle) SetMaxOpenConns(count int) { db.raw.SetMaxOpenConns(count) }
func (db *handle) Stats() database.Stats {
	stats := db.raw.Stats()
	return database.Stats{MaxOpenConnections: stats.MaxOpenConnections, InUse: stats.InUse}
}

func (db *handle) BeginTx(ctx context.Context, options *database.TxOptions) (database.Tx, error) {
	var sqlOptions *sql.TxOptions
	if options != nil {
		isolation := sql.LevelDefault
		switch options.Isolation {
		case database.LevelDefault:
		case database.LevelSerializable:
			isolation = sql.LevelSerializable
		default:
			return nil, fmt.Errorf("%w: %d", errUnsupportedIsolation, options.Isolation)
		}
		sqlOptions = &sql.TxOptions{Isolation: isolation, ReadOnly: options.ReadOnly}
	}
	raw, err := db.raw.BeginTx(ctx, sqlOptions)
	if err != nil {
		return nil, fmt.Errorf("begin sqlite transaction: %w", err)
	}
	return &transaction{raw: raw}, nil
}

func (db *handle) BeginImmediate(ctx context.Context) (database.Tx, error) {
	connection, err := db.raw.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire sqlite connection: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, errors.Join(fmt.Errorf("begin immediate sqlite transaction: %w", err), connection.Close())
	}
	return &immediateTransaction{connection: connection, ctx: ctx}, nil
}

func (tx *transaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := tx.raw.ExecContext(ctx, query, args...)
	return wrapResult("execute sqlite transaction query", result, err)
}

func (tx *transaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := tx.raw.QueryContext(ctx, query, args...)
	return wrapRows("query sqlite transaction", rows, err)
}

func (tx *transaction) PrepareContext(ctx context.Context, query string) (database.Stmt, error) {
	statement, err := tx.raw.PrepareContext(ctx, query)
	return wrapStmt("prepare sqlite transaction query", statement, err)
}

func (tx *transaction) Commit() error {
	if err := tx.raw.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return nil
}

func (tx *transaction) Rollback() error {
	if err := tx.raw.Rollback(); err != nil {
		return fmt.Errorf("rollback sqlite transaction: %w", err)
	}
	return nil
}

func (tx *immediateTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := tx.connection.ExecContext(ctx, query, args...)
	return wrapResult("execute immediate sqlite transaction query", result, err)
}

func (tx *immediateTransaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := tx.connection.QueryContext(ctx, query, args...)
	return wrapRows("query immediate sqlite transaction", rows, err)
}

func (tx *immediateTransaction) PrepareContext(ctx context.Context, query string) (database.Stmt, error) {
	statement, err := tx.connection.PrepareContext(ctx, query)
	return wrapStmt("prepare immediate sqlite transaction query", statement, err)
}

func (tx *immediateTransaction) Commit() error {
	if tx.done {
		return sql.ErrTxDone
	}
	if _, err := tx.connection.ExecContext(tx.ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit immediate sqlite transaction: %w", err)
	}
	tx.done = true
	if err := tx.connection.Close(); err != nil {
		return fmt.Errorf("release immediate sqlite connection: %w", err)
	}
	return nil
}

func (tx *immediateTransaction) Rollback() error {
	if tx.done {
		return sql.ErrTxDone
	}
	tx.done = true
	_, err := tx.connection.ExecContext(context.WithoutCancel(tx.ctx), "ROLLBACK")
	if err != nil {
		err = fmt.Errorf("rollback immediate sqlite transaction: %w", err)
	}
	return errors.Join(err, tx.connection.Close())
}

func wrapResult(operation string, result sql.Result, err error) (sql.Result, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return result, nil
}

func wrapRows(operation string, rows *sql.Rows, err error) (*sql.Rows, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return rows, nil
}

func wrapStmt(operation string, statement *sql.Stmt, err error) (database.Stmt, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return statement, nil
}
