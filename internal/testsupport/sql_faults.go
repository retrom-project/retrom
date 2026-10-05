package testsupport

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"testing"

	dbapi "retrom/internal/database"
	dbpostgres "retrom/internal/database/postgres"

	"retrom/internal/testsupport/testpostgres"

	"github.com/jackc/pgx/v5/stdlib"
)

// SQLFaultHooks inject failures at the driver boundary while repositories still
// receive a transaction interface. Hooks may run concurrently and must synchronize state.
// A hook must match the intended statement and bound arguments, never all writes.
type SQLFaultHooks struct {
	BeforeQuery func(context.Context, string, []driver.NamedValue) error
	AfterQuery  func(context.Context, string, []driver.NamedValue, driver.Rows) (driver.Rows, error)
	BeforeExec  func(context.Context, string, []driver.NamedValue) error
	AfterExec   func(context.Context, string, []driver.NamedValue, driver.Result) (driver.Result, error)
}

// OpenSQLFaultDatabase opens another connection pool to the isolated test database.
// It does not register a process-global driver, mutate the schema, or replace the
// original pool. The caller wires this pool only into the consumer under test.
func OpenSQLFaultDatabase(t testing.TB, source dbapi.DB, hooks SQLFaultHooks) dbapi.DB {
	t.Helper()
	var name string
	if err := dbapi.QueryRowContext(t.Context(), source, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	connector := sqlFaultConnector{
		base: stdlib.GetDefaultDriver(), dsn: testpostgres.ForDatabase(t, name), hooks: hooks,
	}
	database := dbpostgres.OpenConnector(connector, dbpostgres.Options{MaxOpenConns: 1})
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
		if err := connection.hooks.BeforeExec(ctx, faultTemplate(query), args); err != nil {
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
		return connection.hooks.AfterExec(ctx, faultTemplate(query), args, result)
	}
	return result, nil
}

func (connection sqlFaultConnection) QueryContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Rows, error) {
	if connection.hooks.BeforeQuery != nil {
		if err := connection.hooks.BeforeQuery(ctx, faultTemplate(query), args); err != nil {
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
	if connection.hooks.AfterQuery != nil {
		projected, err := connection.hooks.AfterQuery(ctx, faultTemplate(query), args, rows)
		if err != nil {
			return nil, fmt.Errorf("intercept query rows: %w", errors.Join(err, rows.Close()))
		}
		return projected, nil
	}
	return rows, nil
}

// Hooks match the composed query template before driver parameter numbering.
var parameterNumber = regexp.MustCompile(`\$[0-9]+`)

func faultTemplate(query string) string { return parameterNumber.ReplaceAllString(query, "?") }

// Preserve the driver's pool lifecycle, including cancellation-discarded connections.
func (connection sqlFaultConnection) IsValid() bool {
	validator, ok := connection.Conn.(driver.Validator)
	return !ok || validator.IsValid()
}

func (connection sqlFaultConnection) ResetSession(ctx context.Context) error {
	if resetter, ok := connection.Conn.(driver.SessionResetter); ok {
		if err := resetter.ResetSession(ctx); err != nil {
			return fmt.Errorf("reset fault connection: %w", err)
		}
	}
	return nil
}
