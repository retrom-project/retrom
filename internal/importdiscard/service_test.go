package importdiscard

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
	"retrom/internal/testsupport"
)

const adminID = "01980000-0000-7000-8000-000000009992"

type fixture struct {
	ctx      context.Context
	db       *sql.DB
	blobs    *blobstore.Store
	importer *libraryimport.Service
	service  *Service
	releases *payloadrelease.Service
	now      func() time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(1788000000000) }
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: adminID, Role: "ADMIN"})
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dir, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	deps, err := dependencies.Load(filepath.Join(filepath.Dir(filename), "../../data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Bootstrap(ctx, database.SQL, now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{ctx: ctx, db: database.SQL, blobs: blobs, now: now}
	f.exec(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('discard-profile','Discard',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'discard-profile','discard-admin','Discard','ADMIN','ENABLED',0,0)`, adminID)
	f.importer = libraryimport.New(f.db, now).WithBlobStore(blobs)
	f.service = New(f.db, f.importer, nil, nil, now)
	f.releases, err = payloadrelease.New(f.db, blobs, now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) file(t *testing.T, name string, payload byte) libraryimport.ServerSourceFile {
	t.Helper()
	blob, err := f.blobs.Put(bytes.NewReader(bytes.Repeat([]byte{payload}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	id, err := blobstore.EnsureRecord(f.ctx, f.db, blob, "application/octet-stream", f.now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	return libraryimport.ServerSourceFile{RelativePath: name, BlobID: id, SizeBytes: blob.Size}
}

func (f *fixture) create(t *testing.T, files ...libraryimport.ServerSourceFile) libraryimport.ServerImportResult {
	t.Helper()
	result, err := f.importer.CreateServerSource(f.ctx, testsupport.MustPlatformInstanceID(t, f.db, "nes/fceumm"), "STANDARD", files, nil, adminID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *fixture) finish(t *testing.T, kind, id string) {
	t.Helper()
	for range 30 {
		if _, err := f.service.RunOnce(f.ctx); err != nil {
			t.Fatal(err)
		}
		for range 20 {
			worked, err := f.releases.RunOnce(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				break
			}
		}
		status, err := f.service.Get(f.ctx, kind, id)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "COMPLETED" {
			return
		}
	}
	t.Fatal("discard did not finish within bounded reconciliation")
}

func TestDiscardRejectedOnlyBatchReleasesUploadAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	file := f.file(t, "unsupported.txt", 1)
	result := f.create(t, file)
	if len(result.Items) != 0 || len(result.RejectedCodes) != 1 {
		t.Fatalf("expected rejected-only import: %#v", result)
	}
	for range 2 {
		if _, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID); err != nil {
			t.Fatal(err)
		}
	}
	// Recreate the coordinator to prove the request does not depend on the HTTP request/browser.
	f.service = New(f.db, f.importer, nil, nil, f.now)
	f.finish(t, "IMPORT", result.Created.ImportJobID)
	if n := f.count(t, `SELECT count(*) FROM upload_consumptions WHERE consumer_id=? AND released_at_ms IS NULL`, result.Created.ImportJobID); n != 0 {
		t.Fatalf("active consumption: %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM upload_files WHERE final_blob_id=?`, file.BlobID); n != 0 {
		t.Fatalf("protected upload: %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM import_job_files WHERE import_job_id=? AND disposition='REJECTED' AND reason_code='UNSUPPORTED_CONTENT_FORMAT'`, result.Created.ImportJobID); n != 1 {
		t.Fatal("rejection evidence was lost")
	}
	if n := f.count(t, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`, file.BlobID); n != 1 {
		t.Fatal("released bytes were not scheduled for GC")
	}
	if _, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_events WHERE action='IMPORT_BATCH_DISCARD_REQUESTED' AND resource_id=?`, result.Created.ImportJobID); n != 1 {
		t.Fatal("repeated request created duplicate audit records")
	}
}

func TestDiscardPreservesPublishedGameAndOtherBatch(t *testing.T) {
	f := newFixture(t)
	result := f.create(t, f.file(t, "published.nes", 2), f.file(t, "discard.nes", 3), f.file(t, "bad.txt", 4))
	if len(result.Items) != 2 {
		t.Fatalf("reviews: %#v", result)
	}
	published, discarded := result.Items[0], result.Items[1]
	if _, err := f.importer.Approve(f.ctx, published.ItemID, 1); err != nil {
		t.Fatal(err)
	}
	other := f.create(t, f.file(t, "other.nes", 5))
	if _, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.importer.Approve(f.ctx, discarded.ItemID, 1); err == nil {
		t.Fatal("publication raced past discard fence")
	}
	f.finish(t, "IMPORT", result.Created.ImportJobID)
	if n := f.count(t, `SELECT count(*) FROM import_items WHERE id=? AND state='DISCARDED'`, discarded.ItemID); n != 1 {
		t.Fatal("review not discarded")
	}
	if n := f.count(t, `SELECT count(*) FROM review_events WHERE import_item_id=? AND event_type='DISCARDED'`, discarded.ItemID); n != 1 {
		t.Fatal("missing ordinary discard decision")
	}
	if n := f.count(t, `SELECT count(*) FROM games WHERE status='PUBLISHED'`); n != 1 {
		t.Fatal("published game changed")
	}
	if n := f.count(t, `SELECT count(*) FROM import_items WHERE import_job_id=? AND state='REVIEW_PENDING'`, other.Created.ImportJobID); n != 1 {
		t.Fatal("unrelated batch changed")
	}
	if n := f.count(t, `SELECT count(*) FROM game_files file JOIN blob_gc_candidates gc ON gc.blob_id=file.blob_id`); n != 0 {
		t.Fatal("published content entered GC")
	}
}

func TestDiscardWaitsForExecutionStopAndResumesAfterRestart(t *testing.T) {
	f := newFixture(t)
	result := f.create(t, f.file(t, "pending.nes", 20))
	f.exec(t, `UPDATE jobs SET state='RUNNING',finished_at_ms=NULL WHERE scope_id=? AND kind='IMPORT_GROUP'`, result.Created.ImportJobID)
	f.exec(t, `UPDATE import_jobs SET state='RUNNING' WHERE id=?`, result.Created.ImportJobID)
	if _, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RunOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM import_jobs WHERE id=? AND state='CANCEL_REQUESTED'`, result.Created.ImportJobID); n != 1 {
		t.Fatal("running import did not receive cancellation")
	}
	if n := f.count(t, `SELECT count(*) FROM import_items WHERE import_job_id=? AND state='REVIEW_PENDING'`, result.Created.ImportJobID); n != 1 {
		t.Fatal("review was destroyed before explicit disposition")
	}
	f.importer.RecoverImportGroupJobs(f.ctx)
	f.service = New(f.db, f.importer, nil, nil, f.now)
	f.finish(t, "IMPORT", result.Created.ImportJobID)
	if n := f.count(t, `SELECT count(*) FROM review_events WHERE event_type='DISCARDED'`); n != 1 {
		t.Fatal("restart lost or duplicated discard decision")
	}
}

func TestDiscardKeepsSharedPendingContentProtected(t *testing.T) {
	f := newFixture(t)
	file := f.file(t, "shared.nes", 21)
	first := f.create(t, file)
	second := f.create(t, file)
	if _, err := f.service.Request(f.ctx, "IMPORT", first.Created.ImportJobID, adminID); err != nil {
		t.Fatal(err)
	}
	f.finish(t, "IMPORT", first.Created.ImportJobID)
	if n := f.count(t, `SELECT count(*) FROM import_item_source_files WHERE import_item_id IN
 (SELECT id FROM import_items WHERE import_job_id=?) AND blob_id=?`, second.Created.ImportJobID, file.BlobID); n != 1 {
		t.Fatal("another review lost its content")
	}
	if n := f.count(t, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`, file.BlobID); n != 0 {
		t.Fatal("shared pending content entered GC")
	}
}

func TestDiscardUnavailableWhenEveryItemAlreadyDecided(t *testing.T) {
	f := newFixture(t)
	result := f.create(t, f.file(t, "approved.nes", 31), f.file(t, "discarded.nes", 32))
	if len(result.Items) != 2 {
		t.Fatal("expected two reviews")
	}
	if _, err := f.importer.Approve(f.ctx, result.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.importer.Discard(f.ctx, result.Items[1].ItemID, 1, "done"); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.Get(f.ctx, "IMPORT", result.Created.ImportJobID)
	if err != nil || status.State != "UNAVAILABLE" {
		t.Fatalf("already decided availability=%+v error=%v", status, err)
	}
	if _, err := f.service.Request(f.ctx, "IMPORT", result.Created.ImportJobID, adminID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("already decided request=%v", err)
	}
}
