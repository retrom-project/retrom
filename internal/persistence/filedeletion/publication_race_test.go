package filedeletion_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filedeletion"
	"retrom/internal/filestore"
	"retrom/internal/persistence/cleanupjobs"
	"retrom/internal/persistence/filecatalog"
	storage "retrom/internal/persistence/filedeletion"
	"retrom/internal/persistence/fileownership"
	application "retrom/internal/service/cleanupjobs"
	"retrom/internal/store"
)

type fileDeletionCommitHook struct {
	application.FileDeletionRepository
	after func() error
}

func (hook fileDeletionCommitHook) WithFileDeletion(ctx context.Context, work func(application.FileDeletionScope) error) error {
	if err := hook.FileDeletionRepository.WithFileDeletion(ctx, work); err != nil {
		return err
	}
	return hook.after()
}

type fileDeletionAuthority struct {
	checks int
	late   error
}

func (authority *fileDeletionAuthority) CheckInScope(context.Context, application.WorkerScope, application.Work) error {
	authority.checks++
	if authority.checks == 2 {
		return authority.late
	}
	return nil
}

func fileDeletionRaceFixture(t *testing.T) (dbapi.DB, *filestore.Store, filestore.Metadata, application.Execution) {
	t.Helper()
	root := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(root, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	blobs, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := blobs.Put(bytes.NewBufferString("concurrent identical publication"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := filecatalog.EnsureRecord(t.Context(), database.SQL, metadata, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	owner := fileownership.Owner{Kind: "GAME", ID: "old-game"}
	if err := fileownership.Adopt(t.Context(), database.SQL, id, owner); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.RetireAll(t.Context(), database.SQL, owner, 10); err != nil {
		t.Fatal(err)
	}
	deletion, err := application.NewDeletionScheduler(storage.NewQueue(database.SQL, cleanupjobs.BindWorker), application.DeletionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := deletion.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := dbapi.QueryRowContext(t.Context(), database.SQL,
		`SELECT deletion_job_id FROM file_deletions WHERE blob_id=?`, id).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	work, found, err := cleanupjobs.BindWorker(database.SQL).Read.Current(t.Context(), jobID)
	if err != nil || !found {
		t.Fatalf("file deletion work: found=%v error=%v", found, err)
	}
	input, err := application.DecodeWork(work)
	if err != nil {
		t.Fatal(err)
	}
	return database.SQL, blobs, metadata, application.Execution{Work: work, Input: input}
}

func TestOldFileDeletionCannotDeleteSameDigestPublishedAfterCatalogCommit(t *testing.T) {
	db, blobs, _, unit := fileDeletionRaceFixture(t)
	var replacement string
	repository := fileDeletionCommitHook{
		FileDeletionRepository: storage.NewFileDeletion(db, cleanupjobs.BindWorker),
		after: func() error {
			if replacement != "" {
				return nil
			}
			metadata, err := blobs.Put(bytes.NewBufferString("concurrent identical publication"))
			if err != nil {
				return err
			}
			return dbapi.InTransaction(t.Context(), db, nil, func(tx dbapi.Tx) error {
				replacement, err = filecatalog.EnsureRecord(t.Context(), tx, metadata, "application/octet-stream", 10)
				if err != nil {
					return err
				}
				return fileownership.Adopt(t.Context(), tx, replacement, fileownership.Owner{Kind: "GAME", ID: "new-game"})
			})
		},
	}
	collector := application.NewFileDeletionCollector(repository, &fileDeletionAuthority{}, filedeletion.New(blobs))
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	if replacement == unit.Work.Scope.ID || replacement == "" {
		t.Fatal("test did not interleave a new Blob registration")
	}
	// Replaying the old execution must also leave the replacement alone.
	collector = application.NewFileDeletionCollector(storage.NewFileDeletion(db, cleanupjobs.BindWorker),
		&fileDeletionAuthority{}, filedeletion.New(blobs))
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(blobs.Path(replacement))
	if err != nil || string(contents) != "concurrent identical publication" {
		t.Fatalf("old file deletion deleted the new publication: %q error=%v", contents, err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT owner_kind='GAME' AND owner_id='new-game' AND retired_at_ms IS NULL FROM stored_files WHERE id=?`, replacement).
		Scan(&count); err != nil || count != 1 {
		t.Fatalf("replacement reference count=%d error=%v", count, err)
	}
}

func TestFileDeletionRetriesCatalogSettlementAfterLostAuthority(t *testing.T) {
	db, blobs, metadata, unit := fileDeletionRaceFixture(t)
	authority := &fileDeletionAuthority{late: application.ErrExecutionLost}
	collector := application.NewFileDeletionCollector(storage.NewFileDeletion(db, cleanupjobs.BindWorker),
		authority, filedeletion.New(blobs))
	if err := collector.Execute(t.Context(), unit); !errors.Is(err, application.ErrExecutionLost) {
		t.Fatalf("authority error=%v", err)
	}
	if _, err := os.Stat(metadata.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired file remains after deletion: %v", err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT count(*) FROM stored_files WHERE id=?`, unit.Work.Scope.ID).
		Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback lost Blob catalog: %d error=%v", count, err)
	}
	authority.late = nil
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT count(*) FROM stored_files WHERE id=?`, unit.Work.Scope.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retry did not settle deletion: %d %v", count, err)
	}
}

func TestRegistrationRejectsPublicationRetiredBeforeReferenceTransaction(t *testing.T) {
	db, blobs, metadata, unit := fileDeletionRaceFixture(t)
	collector := application.NewFileDeletionCollector(storage.NewFileDeletion(db, cleanupjobs.BindWorker),
		&fileDeletionAuthority{}, filedeletion.New(blobs))
	if err := collector.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	if _, err := filecatalog.EnsureRecord(t.Context(), db, metadata, "application/octet-stream", 10); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing publication registered successfully: %v", err)
	}
}
