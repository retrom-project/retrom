// Package postgres adapts database/sql's PostgreSQL handles to Retrom's database contract.
package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"retrom/internal/database"
	"retrom/internal/telemetry"
)

type Options struct {
	MaxOpenConns int
	MaxIdleConns int
	Now          func() time.Time
	ReadOnly     bool
}

type (
	handle struct {
		raw          *sql.DB
		coordination *sql.DB
		readOnly     bool
		now          func() time.Time
		observations observations
	}
	transaction struct {
		raw              *sql.Tx
		connection       *sql.Conn
		stopCancellation func() bool
		owner            string
		now              func() time.Time
		database         *handle
		context          context.Context
		started          time.Time
		beginDuration    time.Duration
		sqlDuration      atomic.Int64
		rowsDuration     atomic.Int64
		commitDuration   time.Duration
		observed         atomic.Bool
		readOnly         bool
	}
)

var (
	_ database.DB = (*handle)(nil)
	_ database.Tx = (*transaction)(nil)
)

func Open(dsn string, options Options) (database.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	config.RuntimeParams["application_name"] = "retrom"
	config.RuntimeParams["timezone"] = "UTC"
	config.RuntimeParams["lock_timeout"] = "5s"
	if options.ReadOnly {
		config.RuntimeParams["default_transaction_read_only"] = "on"
	}
	raw := stdlib.OpenDB(*config)
	configure(raw, options)
	coordination := stdlib.OpenDB(*config)
	configure(coordination, options)
	return &handle{raw: raw, coordination: coordination, now: clock(options), readOnly: options.ReadOnly}, nil
}

// OpenConnector supports driver-boundary fault injection without exposing a SQL pool.
func OpenConnector(connector driver.Connector, options Options) database.DB {
	raw := sql.OpenDB(connector)
	configure(raw, options)
	coordination := sql.OpenDB(connector)
	configure(coordination, options)
	return &handle{raw: raw, coordination: coordination, now: clock(options), readOnly: options.ReadOnly}
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

func (db *handle) connection(ctx context.Context) (*sql.Conn, error) {
	started := time.Now()
	connection, err := db.raw.Conn(ctx)
	elapsed := time.Since(started)
	telemetry.RecordTiming(ctx, telemetry.PoolWait, elapsed)
	phase := telemetry.WriterPoolWait
	if db.readOnly {
		phase = telemetry.ReaderPoolWait
	}
	telemetry.RecordTiming(ctx, phase, elapsed)
	if err != nil {
		return nil, fmt.Errorf("acquire postgres connection: %w", err)
	}
	return connection, nil
}

func (db *handle) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	connection, err := db.connection(ctx)
	if err != nil {
		db.observeSQL(ctx, time.Now())
		return nil, err
	}
	defer func() { _ = connection.Close() }()
	defer db.observeSQL(ctx, time.Now())
	result, err := connection.ExecContext(ctx, bindQuery(query), arguments(args)...)
	return wrapResult("execute postgres query", result, err)
}

func (db *handle) QueryContext(ctx context.Context, query string, args ...any) (database.Rows, error) {
	connection, err := db.connection(ctx)
	if err != nil {
		db.observeSQL(ctx, time.Now())
		return nil, err
	}
	started := time.Now()
	rows, err := connection.QueryContext(ctx, bindQuery(query), arguments(args)...)
	db.observeSQL(ctx, started)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("query postgres database: %w", err)
	}
	if err := rows.Err(); err != nil {
		defer func() { _ = rows.Close() }()
		_ = connection.Close()
		return nil, fmt.Errorf("open postgres rows: %w", err)
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = rows.Close(); _ = connection.Close() })
	return &observedRows{Rows: rows, ctx: ctx, connection: connection, stopCancellation: stopCancellation}, nil
}

func (db *handle) PingContext(ctx context.Context) error {
	if err := db.raw.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres database: %w", err)
	}
	return nil
}

func (db *handle) Close() error {
	if err := errors.Join(db.raw.Close(), db.coordination.Close()); err != nil {
		return fmt.Errorf("close postgres database: %w", err)
	}
	return nil
}

func (db *handle) SetMaxOpenConns(count int) { db.raw.SetMaxOpenConns(count) }
func (db *handle) Stats() database.Stats {
	stats := db.raw.Stats()
	return database.Stats{
		MaxOpenConnections: stats.MaxOpenConnections, InUse: stats.InUse,
		WaitCount: stats.WaitCount, WaitDuration: stats.WaitDuration,
		SQLCalls: db.observations.sqlCalls.Load(), SQLCallDuration: time.Duration(db.observations.sqlDuration.Load()),
		Transactions:        db.observations.transactions.Load(),
		TransactionDuration: time.Duration(db.observations.transactionDuration.Load()),
	}
}

func (db *handle) BeginTx(ctx context.Context, options *database.TxOptions) (database.Tx, error) {
	sqlOptions := &sql.TxOptions{Isolation: sql.LevelSerializable}
	if options != nil && options.ReadOnly {
		sqlOptions.ReadOnly = true
		sqlOptions.Isolation = sql.LevelRepeatableRead
	}
	started := time.Now()
	connection, err := db.connection(ctx)
	if err != nil {
		return nil, err
	}
	beginStarted := time.Now()
	raw, err := connection.BeginTx(ctx, sqlOptions)
	telemetry.RecordTiming(ctx, telemetry.Begin, time.Since(beginStarted))
	acquisition := time.Since(started)
	db.observeAcquisition(ctx, acquisition)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("begin postgres transaction: %w", err)
	}
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = raw.Rollback()
		_ = connection.Close()
	})
	tx := &transaction{
		stopCancellation: stopCancellation,
		raw:              raw, connection: connection, owner: transactionOwner(), now: db.now,
		database: db, context: ctx, started: time.Now(),
		beginDuration: acquisition, readOnly: options != nil && options.ReadOnly,
	}
	return database.ObserveTransaction(ctx, tx, options), nil
}

func (tx *transaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	defer tx.observeSQL(time.Now())
	result, err := tx.raw.ExecContext(ctx, bindQuery(query), arguments(args)...)
	return wrapResult("execute postgres transaction query", result, err)
}

func (tx *transaction) QueryContext(ctx context.Context, query string, args ...any) (database.Rows, error) {
	started := time.Now()
	rows, err := tx.raw.QueryContext(ctx, bindQuery(query), arguments(args)...)
	tx.observeSQL(started)
	if err != nil {
		return nil, fmt.Errorf("query postgres transaction: %w", err)
	}
	if err := rows.Err(); err != nil {
		defer func() { _ = rows.Close() }()
		return nil, fmt.Errorf("open postgres transaction rows: %w", err)
	}
	return &observedRows{Rows: rows, ctx: ctx, tx: tx}, nil
}

func (tx *transaction) PrepareContext(ctx context.Context, query string) (database.Stmt, error) {
	defer tx.observeSQL(time.Now())
	statement, err := tx.raw.PrepareContext(ctx, bindQuery(query))
	if err != nil {
		return nil, fmt.Errorf("prepare postgres transaction query: %w", err)
	}
	return &preparedStatement{raw: statement, transaction: tx}, nil
}

func (tx *transaction) Commit() error {
	defer tx.observeEnd("commit")
	started := time.Now()
	defer func() {
		tx.commitDuration = time.Since(started)
		telemetry.RecordTiming(tx.context, telemetry.Commit, tx.commitDuration)
	}()
	if err := tx.raw.Commit(); err != nil {
		return fmt.Errorf("commit postgres transaction: %w", err)
	}
	return nil
}

func (tx *transaction) Rollback() error {
	defer tx.observeEnd("rollback")
	if err := tx.raw.Rollback(); err != nil {
		return fmt.Errorf("rollback postgres transaction: %w", err)
	}
	return nil
}

func wrapResult(operation string, result sql.Result, err error) (sql.Result, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return result, nil
}

type preparedStatement struct {
	raw         *sql.Stmt
	transaction *transaction
}

func (statement *preparedStatement) ExecContext(ctx context.Context, args ...any) (sql.Result, error) {
	defer statement.transaction.observeSQL(time.Now())
	result, err := statement.raw.ExecContext(ctx, arguments(args)...)
	return wrapResult("execute prepared postgres query", result, err)
}

func (statement *preparedStatement) Close() error {
	if err := statement.raw.Close(); err != nil {
		return fmt.Errorf("close prepared postgres query: %w", err)
	}
	return nil
}
