//go:build integration

package libraryimport

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
)

func metadataFixture(t *testing.T) (deduplicateFixture, string) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	result := fixture.create(t, "metadata", "server metadata fixture", 1)
	fixture.service.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return fixture, result.Items[0].ItemID
}

func TestServerMetadataPreservesReadFailure(t *testing.T) {
	fixture, itemID := metadataFixture(t)
	fixture.execute(t, `UPDATE review_drafts SET version='broken' WHERE import_item_id=?`, itemID)
	version, _, err := fixture.service.SeedServerReviewMetadata(fixture.ctx, itemID, ServerMetadata{Title: "Changed"})
	if err == nil || errors.Is(err, ErrInvalid) || version != 0 {
		t.Fatalf("read failure incorrectly mapped: version=%d error=%v", version, err)
	}
}

type metadataEntropyFailure struct{}

func (metadataEntropyFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestServerMetadataRejectsEntropyFailureWithoutSaving(t *testing.T) {
	fixture, itemID := metadataFixture(t)
	var before string
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT metadata_json FROM review_drafts WHERE import_item_id=?`, itemID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var version int64
	var err error
	func() {
		uuid.SetRand(metadataEntropyFailure{})
		defer uuid.SetRand(nil)
		version, _, err = fixture.service.SeedServerReviewMetadata(fixture.ctx, itemID, ServerMetadata{Title: "Changed"})
	}()
	if !errors.Is(err, io.ErrUnexpectedEOF) || version != 0 {
		t.Fatalf("entropy failure saved draft: version=%d error=%v", version, err)
	}
	var after string
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT metadata_json FROM review_drafts WHERE import_item_id=?`, itemID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("entropy failure changed draft")
	}
}

func TestServerMetadataUsesFrozenMaximumYear(t *testing.T) {
	fixture, itemID := metadataFixture(t)
	year := 2028
	version, warnings, err := fixture.service.SeedServerReviewMetadataAtYear(fixture.ctx, itemID, ServerMetadata{Title: "Changed", ReleaseYear: &year}, year)
	if err != nil || version != 2 || len(warnings) != 0 {
		t.Fatalf("frozen year rejected: version=%d warnings=%#v error=%v", version, warnings, err)
	}
	var stored int
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT json_extract(metadata_json,'$.releaseYear') FROM review_drafts WHERE import_item_id=?`, itemID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != year {
		t.Fatalf("release year=%d want%d", stored, year)
	}
}
