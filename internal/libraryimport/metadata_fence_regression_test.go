//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	dbsqlite "retrom/internal/database/sqlite"

	"modernc.org/sqlite"
)

type metadataFaultConnector struct {
	path       string
	countError error
}

func (connector metadataFaultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := connector.Driver().Open(connector.path)
	if err != nil {
		return nil, err
	}
	return metadataFaultConnection{Conn: conn, countError: connector.countError}, nil
}
func (metadataFaultConnector) Driver() driver.Driver { return &sqlite.Driver{} }

type metadataFaultConnection struct {
	driver.Conn
	countError error
}

func (connection metadataFaultConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "UPDATE import_items SET") {
		return metadataFaultResult{cause: connection.countError}, nil
	}
	executor, ok := connection.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return executor.ExecContext(ctx, query, args)
}

type metadataFaultResult struct{ cause error }

func (metadataFaultResult) LastInsertId() (int64, error)        { return 0, nil }
func (result metadataFaultResult) RowsAffected() (int64, error) { return 0, result.cause }

func TestServerMetadataRequiresSuccessfulDraftCAS(t *testing.T) {
	for _, phase := range []string{"stale draft", "affected count failure"} {
		t.Run(phase, func(t *testing.T) { assertMetadataDraftCAS(t, phase) })
	}
}

func assertMetadataDraftCAS(t *testing.T, phase string) {
	t.Helper()
	fixture, itemID := metadataFixture(t)
	var ordinal int
	var name, path string
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `PRAGMA database_list`).Scan(&ordinal, &name, &path); err != nil {
		t.Fatal(err)
	}
	var cause error
	if phase == "affected count failure" {
		cause = errors.New("affected row count unavailable")
	}
	intercepted := dbsqlite.OpenConnector(metadataFaultConnector{path: path, countError: cause}, dbsqlite.Options{})
	t.Cleanup(func() {
		if err := intercepted.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.service = newTestImporter(t, intercepted, fixture.service.blobs, testImportOptions{Now: fixture.service.now, MultiDiscEnabled: fixture.service.multiDiscImportEnabled})
	version, _, err := fixture.service.SeedServerReviewMetadata(fixture.ctx, itemID, ServerMetadata{Title: "Changed"})
	expected := cause
	if expected == nil {
		expected = ErrVersionConflict
	}
	if !errors.Is(err, expected) || version != 0 {
		t.Fatalf("%s ignored: version=%d error=%v", phase, version, err)
	}
	var changed int
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT version-1 FROM import_items WHERE id=?`, itemID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("failed draft mutation added %d events", changed)
	}
}
