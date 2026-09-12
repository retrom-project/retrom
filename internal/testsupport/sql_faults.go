package testsupport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
)

// SQLFaultHooks inject failures at the driver boundary while repositories still
// receive a real *sql.Tx. Hooks may run concurrently and must synchronize state.
// A hook must match the intended statement and bound arguments, never all writes.
type SQLFaultHooks struct {
	BeforeQuery func(context.Context, string, []driver.NamedValue) error
	BeforeExec  func(context.Context, string, []driver.NamedValue) error
	AfterExec   func(context.Context, string, []driver.NamedValue, driver.Result) (driver.Result, error)
}

// OpenSQLFaultDatabase opens another connection pool to a file-backed test database.
// It does not register a process-global driver, mutate the schema, or replace the
// original pool. The caller wires this pool only into the consumer under test.
func OpenSQLFaultDatabase(t testing.TB, source *sql.DB, hooks SQLFaultHooks) *sql.DB {
	t.Helper()
	var sequence int
	var name, filename string
	if err := source.QueryRowContext(t.Context(), `PRAGMA database_list`).Scan(&sequence, &name, &filename); err != nil {
		t.Fatal(err)
	}
	if name != "main" || filename == "" {
		t.Fatal("SQL fault injection requires a file-backed test database")
	}
	connector := sqlFaultConnector{
		base: source.Driver(), dsn: filename + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", hooks: hooks,
	}
	database := sql.OpenDB(connector)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return database
}

type sqlFaultConnector struct {
	base  driver.Driver
	dsn   string
	hooks SQLFaultHooks
}

func (connector sqlFaultConnector) Driver() driver.Driver { return connector.base }
func (connector sqlFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("connect SQL fault pool: %w", err)
	}
	connection, err := connector.base.Open(connector.dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQL fault connection: %w", err)
	}
	return sqlFaultConnection{Conn: connection, hooks: connector.hooks}, nil
}

type sqlFaultConnection struct {
	driver.Conn
	hooks SQLFaultHooks
}

var errSQLFaultTransactions = errors.New("SQL fault injection requires a context-aware transaction driver")

func (connection sqlFaultConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	begin, ok := connection.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, errSQLFaultTransactions
	}
	transaction, err := begin.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin fault-injected transaction: %w", err)
	}
	return transaction, nil
}

func (connection sqlFaultConnection) ExecContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	if connection.hooks.BeforeExec != nil {
		if err := connection.hooks.BeforeExec(ctx, query, args); err != nil {
			return nil, err
		}
	}
	executor, ok := connection.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	result, err := executor.ExecContext(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("execute fault-injected statement: %w", err)
	}
	if connection.hooks.AfterExec != nil {
		return connection.hooks.AfterExec(ctx, query, args, result)
	}
	return result, nil
}

func (connection sqlFaultConnection) QueryContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Rows, error) {
	if connection.hooks.BeforeQuery != nil {
		if err := connection.hooks.BeforeQuery(ctx, query, args); err != nil {
			return nil, err
		}
	}
	reader, ok := connection.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := reader.QueryContext(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("query fault-injected statement: %w", err)
	}
	return rows, nil
}
