//go:build integration

package filedeletion_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	workerrepo "retrom/internal/persistence/cleanupjobs"
	"retrom/internal/persistence/filedeletion"
	application "retrom/internal/service/cleanupjobs"
	"retrom/internal/store"
	"retrom/internal/testsupport/testpostgres"
)

type interruptedRemoval struct {
	files *filestore.Store
	fail  bool
}

func (removal *interruptedRemoval) Delete(ctx context.Context, path string) error {
	if err := removal.files.RemovePath(ctx, path); err != nil {
		return err
	}
	if removal.fail {
		removal.fail = false
		return errors.New("interrupted after directory removal")
	}
	return nil
}

func TestDirectoryDeletionRetriesWithoutTouchingAnotherGame(t *testing.T) {
	ctx := t.Context()
	now := time.Now()
	clock := func() time.Time { return now }
	root := t.TempDir()
	database, err := store.Open(ctx, testpostgres.DSN(t), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	files, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := files.Put(strings.NewReader("independent game content"))
	if err != nil {
		t.Fatal(err)
	}
	first := filestore.GameDirectory("01980000-0000-7000-8000-000000000011")
	second := filestore.GameDirectory("01980000-0000-7000-8000-000000000012")
	original, err := files.CopyTo(ctx, scratch.Record, first+"/content/01980000-0000-7000-8000-000000000013", "game.rom")
	if err != nil {
		t.Fatal(err)
	}
	other, err := files.CopyTo(ctx, scratch.Record, second+"/content/01980000-0000-7000-8000-000000000014", "game.rom")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := filedeletion.QueuePath(ctx, tx, first, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT count(*) FROM jobs WHERE kind='PATH_DELETE'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback tasks=%d error=%v", count, err)
	}
	tx, err = database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := filedeletion.QueuePath(ctx, tx, first, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT count(*) FROM jobs WHERE kind='PATH_DELETE'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate tasks=%d error=%v", count, err)
	}
	remover := &interruptedRemoval{files: files, fail: true}
	worker, err := application.New(ctx,
		application.Dependencies{Worker: workerrepo.NewWorker(database.SQL), Files: remover},
		application.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	if did, err := worker.RunOnce(ctx); !did || err == nil {
		t.Fatalf("missing interruption: %t %v", did, err)
	}
	if _, err := os.Stat(files.Path(original.Record)); !os.IsNotExist(err) {
		t.Fatalf("old directory survived: %v", err)
	}
	now = now.Add(2 * time.Second)
	if did, err := worker.RunOnce(ctx); !did || err != nil {
		t.Fatalf("retry: %t %v", did, err)
	}
	var state string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT state FROM jobs WHERE kind='PATH_DELETE'`).Scan(&state); err != nil || state != "SUCCEEDED" {
		t.Fatalf("settlement=%s %v", state, err)
	}
	contents, err := os.ReadFile(files.Path(other.Record))
	if err != nil || string(contents) != "independent game content" {
		t.Fatalf("other game damaged: %q %v", contents, err)
	}
}
