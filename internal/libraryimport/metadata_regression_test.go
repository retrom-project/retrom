//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/testsupport"
)

func metadataFixture(t *testing.T) (deduplicateFixture, string) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	result := fixture.create(t, "metadata", "server metadata fixture", 1)
	fixture.service = newTestImporter(t, fixture.service.database, fixture.service.blobs, testImportOptions{Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, MultiDiscEnabled: fixture.service.multiDiscImportEnabled})
	return fixture, result.Items[0].ItemID
}

func TestServerMetadataPreservesReadFailure(t *testing.T) {
	fixture, itemID := metadataFixture(t)
	cause := errors.New("metadata read unavailable")
	hits := 0
	faulty := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "FROM import_items") {
				hits++
				return cause
			}
			return nil
		},
	})
	fixture.service = newTestImporter(t, faulty, fixture.service.blobs, testImportOptions{Now: fixture.service.now, MultiDiscEnabled: fixture.service.multiDiscImportEnabled})
	version, _, err := fixture.service.SeedServerReviewMetadata(fixture.ctx, itemID, ServerMetadata{Title: "Changed"})
	if !errors.Is(err, cause) || hits != 1 || errors.Is(err, ErrInvalid) || version != 0 {
		t.Fatalf("read failure incorrectly mapped: version=%d error=%v", version, err)
	}
}
