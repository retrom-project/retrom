// Package sqlite adapts database/sql's SQLite handles to Retrom's database contract.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Register SQLite for sql.Open in this adapter.

	"retrom/internal/database"
)

type Options struct {
	MaxOpenConns int
	MaxIdleConns int
	Now          func() time.Time
}

type (
	handle struct {
		raw *sql.DB
		now func() time.Time
	}
	transaction struct {
		raw *sql.Tx
		now func() time.Time
	}
)

var errUnsupportedIsolation = errors.New("unsupported transaction isolation")

var (
	_ database.DB = (*handle)(nil)
	_ database.Tx = (*transaction)(nil)
)

func Open(dsn string, options Options) (database.DB, error) {
	name, rawQuery, _ := strings.Cut(dsn, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("parse sqlite connection options: %w", err)
	}
	// The driver reserves the writer at BeginTx; ReadOnly transactions keep BEGIN.
	query.Set("_txlock", "immediate")
	raw, err := sql.Open("sqlite", name+"?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	configure(raw, options)
	return &handle{raw: raw, now: clock(options)}, nil
}

// OpenConnector supports driver-boundary fault injection without exposing a SQL pool.
// The connector must configure the same _txlock=immediate policy as Open.
func OpenConnector(connector driver.Connector, options Options) database.DB {
	raw := sql.OpenDB(connector)
	configure(raw, options)
	return &handle{raw: raw, now: clock(options)}
}

func clock(options Options) func() time.Time {
	if options.Now != nil {
		return options.Now
	}
	return time.Now
}

func (tx *transaction) NowMS() int64 { return tx.now().UnixMilli() }

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
	return &transaction{raw: raw, now: db.now}, nil
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
