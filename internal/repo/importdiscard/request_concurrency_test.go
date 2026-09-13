package importdiscard_test

import (
	"errors"
	"testing"
	"time"

	"retrom/internal/adapter/integration/libraryimport"
	"retrom/internal/bootstrap/composition"
	"retrom/internal/service/importdiscard"
)

func TestDiscardRequestRechecksPublishedBatchBeforeWriting(t *testing.T) {
	f := newFixture(t)
	result := f.create(t, f.file(t, "published.nes", 41))
	f.db.SetMaxOpenConns(2)
	var published bool
	clock := func() time.Time {
		if !published {
			published = true
			if _, err := f.importer.Approve(f.ctx, result.Items[0].ItemID, 1); err != nil {
				t.Fatal(err)
			}
		}
		return f.now()
	}
	f.service = composition.NewImportDiscard(f.db, libraryimport.NewDiscardWorkflow(f.importer), nil, nil, clock)
	_, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID)
	if !errors.Is(err, importdiscard.ErrInvalid) {
		t.Fatalf("already published batch accepted for discard: %v", err)
	}
	if f.count(t, `SELECT count(*) FROM import_batch_discards WHERE import_id=?`, result.Created.ImportJobID) != 0 {
		t.Fatal("stale availability persisted a discard request")
	}
	if f.count(t, `SELECT count(*) FROM audit_events WHERE action='IMPORT_BATCH_DISCARD_REQUESTED'`) != 0 {
		t.Fatal("rejected request recorded an audit event")
	}
}
