//go:build integration

package libraryimport

import (
	"errors"
	"testing"
	"time"
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
	fixture.execute(t, `UPDATE import_items SET review_version='broken' WHERE id=?`, itemID)
	version, _, err := fixture.service.SeedServerReviewMetadata(fixture.ctx, itemID, ServerMetadata{Title: "Changed"})
	if err == nil || errors.Is(err, ErrInvalid) || version != 0 {
		t.Fatalf("read failure incorrectly mapped: version=%d error=%v", version, err)
	}
}
