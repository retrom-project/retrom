//go:build integration

package libraryimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"modernc.org/sqlite"
)

type sourceFaultConnector struct {
	path, phase    string
	cause          error
	beforeCreation func() error
}

func (connector sourceFaultConnector) Driver() driver.Driver { return &sqlite.Driver{} }
func (connector sourceFaultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := connector.Driver().Open(connector.path)
	if err != nil {
		return nil, err
	}
	return &sourceFaultConnection{Conn: conn, phase: connector.phase, cause: connector.cause, beforeCreation: connector.beforeCreation}, nil
}

type sourceFaultConnection struct {
	driver.Conn
	phase          string
	cause          error
	bound          bool
	begins         int
	beforeCreation func() error
}

func (connection *sourceFaultConnection) Begin() (driver.Tx, error) {
	connection.begins++
	if connection.begins == 2 && connection.beforeCreation != nil {
		if err := connection.beforeCreation(); err != nil {
			return nil, err
		}
	}
	tx, err := connection.Conn.Begin()
	if err != nil {
		return nil, err
	}
	connection.bound = false
	return sourceFaultTransaction{Tx: tx, connection: connection}, nil
}

func (connection *sourceFaultConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	binding := strings.HasPrefix(query, "UPDATE pegasus_import_items SET") && strings.Contains(query, "library_import_job_id=")
	if binding {
		connection.bound = true
		switch connection.phase {
		case "write":
			return nil, connection.cause
		case "zero":
			return sourceFaultResult{}, nil
		case "count":
			return sourceFaultResult{connection.cause}, nil
		}
	}
	executor, ok := connection.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return executor.ExecContext(ctx, query, args)
}

type sourceFaultTransaction struct {
	driver.Tx
	connection *sourceFaultConnection
}

func (transaction sourceFaultTransaction) Commit() error {
	if transaction.connection.phase == "commit" && transaction.connection.bound {
		return errors.Join(transaction.connection.cause, transaction.Tx.Rollback())
	}
	return transaction.Tx.Commit()
}

type sourceFaultResult struct{ cause error }

func (sourceFaultResult) LastInsertId() (int64, error)        { return 0, nil }
func (result sourceFaultResult) RowsAffected() (int64, error) { return 0, result.cause }

func TestOwnedSourceRollsBackImportAndBindingOnTransactionFailure(t *testing.T) {
	for _, phase := range []string{"write", "zero", "count", "commit"} {
		t.Run(phase, func(t *testing.T) {
			fixture, request := ownedSourceFixture(t)
			cause := errors.New("injected source " + phase + " failure")
			var ordinal int
			var name, path string
			if err := fixture.database.QueryRowContext(fixture.ctx, `PRAGMA database_list`).Scan(&ordinal, &name, &path); err != nil {
				t.Fatal(err)
			}
			intercepted := sql.OpenDB(sourceFaultConnector{path: path, phase: phase, cause: cause})
			intercepted.SetMaxOpenConns(1)
			t.Cleanup(func() {
				if err := intercepted.Close(); err != nil {
					t.Error(err)
				}
			})
			fixture.service.database = intercepted
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			expected := cause
			if phase == "zero" {
				expected = ErrVersionConflict
			}
			if !errors.Is(err, expected) || result.Created.ImportJobID != "" || result.Items != nil {
				t.Fatalf("%s returned partial success: %#v %v", phase, result, err)
			}
			assertOwnedCreationRolledBack(t, fixture)
		})
	}
}

func assertOwnedCreationRolledBack(t *testing.T, fixture deduplicateFixture) {
	t.Helper()
	var state, jobID, itemID string
	var version, imports, items, drafts int
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT execution_state,COALESCE(library_import_job_id,''),COALESCE(library_import_item_id,''),version,
(SELECT count(*) FROM import_jobs),(SELECT count(*) FROM import_items),(SELECT count(*) FROM review_drafts)
FROM pegasus_import_items WHERE id='unlinked-source'`).Scan(&state, &jobID, &itemID, &version, &imports, &items, &drafts); err != nil {
		t.Fatal(err)
	}
	if state != "COPYING" || jobID != "" || itemID != "" || version != 1 || imports != 0 || items != 0 || drafts != 0 {
		t.Fatalf("failed source mutation persisted: state=%s job=%s item=%s version=%d imports=%d items=%d drafts=%d", state, jobID, itemID, version, imports, items, drafts)
	}
}
