package blobgc_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	dbapi "retrom/internal/database"
	"retrom/internal/payloadfiles"
	"retrom/internal/persistence/blobcatalog"
	storage "retrom/internal/persistence/blobgc"
	"retrom/internal/persistence/payloadworker"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
	"retrom/internal/store"
)

type garbageCommitHook struct {
	application.GarbageRepository
	after func() error
}

func (hook garbageCommitHook) WithGarbage(ctx context.Context, work func(application.GarbageScope) error) error {
	if err := hook.GarbageRepository.WithGarbage(ctx, work); err != nil {
		return err
	}
	return hook.after()
}

type garbageAuthority struct {
	checks int
	late   error
}

func (authority *garbageAuthority) CheckInScope(context.Context, application.WorkerScope, application.Work) error {
	authority.checks++
	if authority.checks == 2 {
		return authority.late
	}
	return nil
}

func garbageRaceFixture(t *testing.T) (dbapi.DB, *blobstore.Store, blobstore.Metadata, application.Execution) {
	t.Helper()
	root := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(root, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := blobs.Put(bytes.NewBufferString("concurrent identical publication"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := blobcatalog.EnsureRecord(t.Context(), database.SQL, metadata, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	gc, err := application.NewGCScheduler(storage.NewGC(database.SQL, payloadworker.BindWorker), application.GCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gc.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := dbapi.QueryRowContext(t.Context(), database.SQL,
		`SELECT gc_job_id FROM blob_gc_candidates WHERE blob_id=?`, id).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	work, found, err := payloadworker.BindWorker(database.SQL).Read.Current(t.Context(), jobID)
	if err != nil || !found {
		t.Fatalf("GC work: found=%v error=%v", found, err)
	}
	input, err := application.DecodeWork(work)
	if err != nil {
		t.Fatal(err)
	}
	return database.SQL, blobs, metadata, application.Execution{Work: work, Input: input}
}

func TestOldGarbageCannotDeleteSameDigestPublishedAfterCatalogCommit(t *testing.T) {
	db, blobs, original, unit := garbageRaceFixture(t)
	var replacement string
	repository := garbageCommitHook{
		GarbageRepository: storage.NewGarbage(db, payloadworker.BindWorker),
		after: func() error {
			metadata, err := blobs.Put(bytes.NewBufferString("concurrent identical publication"))
			if err != nil {
				return err
			}
			return dbapi.InTransaction(t.Context(), db, nil, func(tx dbapi.Tx) error {
				replacement, err = blobcatalog.EnsureRecord(t.Context(), tx, metadata, "application/octet-stream", 10)
				if err != nil {
					return err
				}
				_, err = recordstore.CreateReferences(t.Context(), tx, "metadata_provider_responses", `
 INSERT INTO metadata_provider_responses(id,provider,request_digest,outcome,raw_response_blob_id,
 raw_payload_state,fetched_at_ms,expires_at_ms)
 VALUES('new-owner','HASHEOUS',?,'HIT',?,'RETAINED',10,20)`, metadata.SHA256, replacement)
				return err
			})
		},
	}
	collector := application.NewGarbageCollector(repository, &garbageAuthority{}, payloadfiles.New(blobs))
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	if replacement == unit.Work.Scope.ID || replacement == "" {
		t.Fatal("test did not interleave a new Blob registration")
	}
	// Replaying the old execution must also leave the replacement alone.
	collector = application.NewGarbageCollector(storage.NewGarbage(db, payloadworker.BindWorker),
		&garbageAuthority{}, payloadfiles.New(blobs))
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(original.Path)
	if err != nil || string(contents) != "concurrent identical publication" {
		t.Fatalf("old GC deleted the new publication: %q error=%v", contents, err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT ref_count FROM blobs WHERE id=?`, replacement).
		Scan(&count); err != nil || count != 1 {
		t.Fatalf("replacement reference count=%d error=%v", count, err)
	}
}

func TestGarbageRestoresCanonicalFileWhenAuthorityIsLostBeforeCommit(t *testing.T) {
	db, blobs, metadata, unit := garbageRaceFixture(t)
	authority := &garbageAuthority{late: application.ErrExecutionLost}
	collector := application.NewGarbageCollector(storage.NewGarbage(db, payloadworker.BindWorker),
		authority, payloadfiles.New(blobs))
	if err := collector.Execute(t.Context(), unit); !errors.Is(err, application.ErrExecutionLost) {
		t.Fatalf("authority error=%v", err)
	}
	if _, err := os.Stat(metadata.Path); err != nil {
		t.Fatalf("rollback lost canonical file: %v", err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT count(*) FROM blobs WHERE id=?`, unit.Work.Scope.ID).
		Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback lost Blob catalog: %d error=%v", count, err)
	}
}

func TestRegistrationRejectsPublicationRetiredBeforeReferenceTransaction(t *testing.T) {
	db, blobs, metadata, unit := garbageRaceFixture(t)
	collector := application.NewGarbageCollector(storage.NewGarbage(db, payloadworker.BindWorker),
		&garbageAuthority{}, payloadfiles.New(blobs))
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	if _, err := blobcatalog.EnsureRecord(t.Context(), db, metadata, "application/octet-stream", 10); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing publication registered successfully: %v", err)
	}
}
