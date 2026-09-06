//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/testsupport"
)

type deduplicateFixture struct {
	ctx      context.Context
	database *sql.DB
	service  *Service
	blobs    *blobstore.Store
	platform string
}

func newDeduplicateFixture(t *testing.T) deduplicateFixture {
	t.Helper()
	ctx := t.Context()
	database, blobs, _ := openImportGroupFixture(t, ctx)
	return deduplicateFixture{
		ctx, database.SQL, New(database.SQL, time.Now).WithBlobStore(blobs), blobs,
		testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"),
	}
}

func (fixture deduplicateFixture) create(t *testing.T, name, contents string, count int) ServerImportResult {
	t.Helper()
	metadata, err := fixture.blobs.Put(bytes.NewBufferString(contents))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobstore.EnsureRecord(fixture.ctx, fixture.database, metadata, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	files := make([]ServerSourceFile, count)
	for index := range files {
		files[index] = ServerSourceFile{RelativePath: fmt.Sprintf("%03d/%s.gba", index, name), BlobID: blobID, SizeBytes: metadata.Size}
	}
	result, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != count {
		t.Fatalf("items = %d, want %d", len(result.Items), count)
	}
	return result
}

func (fixture deduplicateFixture) execute(t *testing.T, statement string, args ...any) {
	t.Helper()
	if _, err := fixture.database.ExecContext(fixture.ctx, statement, args...); err != nil {
		t.Fatal(err)
	}
}

func TestReviewDeduplicateDiscardsOnlyPublishedContentAcrossPages(t *testing.T) {
	fixture := newDeduplicateFixture(t)
	original := fixture.create(t, "same-name", "published content", 1)
	copies := fixture.create(t, "renamed-copy", "published content", 52)
	different := fixture.create(t, "same-name", "different content", 1)
	otherScope := fixture.create(t, "other-scope", "published content", 1)
	pendingOnly := fixture.create(t, "pending-only", "no published owner", 2)
	if _, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	scope := ReviewBulkScope{ImportJobID: copies.Created.ImportJobID}
	page, err := fixture.service.DeduplicateReviews(fixture.ctx, ReviewDeduplicateRequest{Scope: scope})
	if err != nil || page.ScannedCount != 50 || page.DiscardedCount != 50 || page.NextAfterItemID == nil {
		t.Fatalf("first page = %#v, %v", page, err)
	}
	last, err := fixture.service.DeduplicateReviews(fixture.ctx, ReviewDeduplicateRequest{
		Scope: scope, AfterItemID: *page.NextAfterItemID, ThroughItemID: *page.ThroughItemID,
	})
	if err != nil || last.ScannedCount != 2 || last.DiscardedCount != 2 || last.NextAfterItemID != nil {
		t.Fatalf("last page = %#v, %v", last, err)
	}
	repeated, err := fixture.service.DeduplicateReviews(fixture.ctx, ReviewDeduplicateRequest{Scope: scope})
	if err != nil || repeated.ScannedCount != 0 || repeated.DiscardedCount != 0 {
		t.Fatalf("repeat = %#v, %v", repeated, err)
	}
	for _, result := range []ServerImportResult{different, otherScope, pendingOnly} {
		for _, item := range result.Items {
			assertDeduplicateItemState(t, fixture, item.ItemID, "REVIEW_PENDING")
		}
	}
	var games, events, discarded, pending int
	if err := fixture.database.QueryRowContext(fixture.ctx, `
SELECT (SELECT count(*) FROM games WHERE status='PUBLISHED'),
 (SELECT count(*) FROM review_events WHERE event_type='DISCARDED'),
 discarded_item_count,review_pending_item_count FROM import_jobs WHERE id=?`, copies.Created.ImportJobID).
		Scan(&games, &events, &discarded, &pending); err != nil {
		t.Fatal(err)
	}
	if games != 1 || events != 52 || discarded != 52 || pending != 0 {
		t.Fatalf("games/events/discarded/pending = %d/%d/%d/%d", games, events, discarded, pending)
	}
	all, err := fixture.service.DeduplicateReviews(fixture.ctx, ReviewDeduplicateRequest{})
	if err != nil || all.DiscardedCount != 1 {
		t.Fatalf("remaining scope = %#v, %v", all, err)
	}
}

func assertDeduplicateItemState(t *testing.T, fixture deduplicateFixture, itemID, expected string) {
	t.Helper()
	var actual string
	if err := fixture.database.QueryRowContext(fixture.ctx, "SELECT state FROM import_items WHERE id=?", itemID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("state = %s, want %s", actual, expected)
	}
}

func TestReviewDeduplicateRollsBackPageOnDiscardFailure(t *testing.T) {
	fixture := newDeduplicateFixture(t)
	original := fixture.create(t, "original", "duplicate contents", 1)
	copies := fixture.create(t, "copies", "duplicate contents", 2)
	if _, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `CREATE TRIGGER deduplicate_fail BEFORE UPDATE OF state ON import_items
WHEN NEW.state='DISCARDED' AND OLD.id=(SELECT max(id) FROM import_items)
BEGIN SELECT RAISE(ABORT,'deduplicate failpoint'); END`)
	request := ReviewDeduplicateRequest{Scope: ReviewBulkScope{ImportJobID: copies.Created.ImportJobID}}
	if _, err := fixture.service.DeduplicateReviews(fixture.ctx, request); err == nil {
		t.Fatal("expected failed discard")
	}
	for _, item := range copies.Items {
		assertDeduplicateItemState(t, fixture, item.ItemID, "REVIEW_PENDING")
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.ctx, "SELECT count(*) FROM review_events WHERE event_type='DISCARDED'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled back discard events = %d", count)
	}
	fixture.execute(t, "DROP TRIGGER deduplicate_fail")
	result, err := fixture.service.DeduplicateReviews(fixture.ctx, request)
	if err != nil || result.DiscardedCount != 2 {
		t.Fatalf("retry = %#v, %v", result, err)
	}
}

func TestReviewDeduplicateSkipsActiveAttachmentsAndOtherPlatforms(t *testing.T) {
	fixture := newDeduplicateFixture(t)
	original := fixture.create(t, "original", "matching payload", 1)
	attachment := fixture.create(t, "attachment", "matching payload", 1)
	otherPlatform := fixture.create(t, "other-platform", "matching payload", 1)
	if _, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `UPDATE review_drafts SET target_platform_instance_id=?,selected_validation_id=NULL,version=version+1 WHERE import_item_id=?`,
		testsupport.MustPlatformInstanceID(t, fixture.database, "nes/fceumm"), otherPlatform.Items[0].ItemID)
	insertArcadeParentCatalog(t, fixture.database)
	itemID := attachment.Items[0].ItemID
	fixture.execute(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
 cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES('deduplicate-attachment-job','IMPORT_ITEM',?,'REVIEW_ARCADE_PARENT_VALIDATE',
 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,'{}',1,'QUEUED',0,2,1,1,1)`, itemID)
	fixture.execute(t, `INSERT INTO review_arcade_parent_attachments(id,import_item_id,review_draft_id,
 base_source_snapshot_id,dependency_machine,expected_logical_name,required_by_machine,depth,
 provider_id,target_id,dat_version_id,original_filename,state,diagnostics_json,job_id,created_at_ms,updated_at_ms)
 SELECT 'deduplicate-attachment',draft.import_item_id,draft.id,draft.effective_source_snapshot_id,
 'b','b.zip','a',1,dat.provider_id,dat.target_id,dat.id,'b.zip','QUEUED','{}','deduplicate-attachment-job',1,1
 FROM review_drafts draft JOIN dat_versions dat ON dat.id='attachment-dat' WHERE draft.import_item_id=?`, itemID)
	result, err := fixture.service.DeduplicateReviews(fixture.ctx, ReviewDeduplicateRequest{})
	if err != nil || result.ScannedCount != 2 || result.AttachmentActiveCount != 1 || result.DiscardedCount != 0 {
		t.Fatalf("skip attachment/other platform = %#v, %v", result, err)
	}
	assertDeduplicateItemState(t, fixture, itemID, "REVIEW_PENDING")
	assertDeduplicateItemState(t, fixture, otherPlatform.Items[0].ItemID, "REVIEW_PENDING")
	var state string
	if err := fixture.database.QueryRowContext(fixture.ctx, "SELECT state FROM review_arcade_parent_attachments WHERE id='deduplicate-attachment'").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" {
		t.Fatalf("attachment was interrupted: %s", state)
	}
}
