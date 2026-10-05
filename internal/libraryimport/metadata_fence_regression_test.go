//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testsupport"

	dbapi "retrom/internal/database"
)

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
	var cause error
	if phase == "affected count failure" {
		cause = errors.New("affected row count unavailable")
	}

	intercepted := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: nil,
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "UPDATE import_items SET") {
				return metadataFaultResult{cause: cause}, nil
			}
			return result, nil
		},
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
