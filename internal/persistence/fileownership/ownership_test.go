package fileownership_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/persistence/filecatalog"
	"retrom/internal/persistence/fileownership"
	"retrom/internal/store"
)

func TestIdenticalFilesHaveIndependentOwnersAndDeletion(t *testing.T) {
	db, files := ownerFixture(t)
	first := registerFile(t, db, files)
	second := registerFile(t, db, files)
	if first.ID == second.ID || first.SHA256 != second.SHA256 {
		t.Fatal("catalog still deduplicates by digest")
	}
	a := fileownership.Owner{Kind: "GAME", ID: "game-a"}
	b := fileownership.Owner{Kind: "GAME", ID: "game-b"}
	if err := fileownership.Adopt(t.Context(), db, first.ID, a); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Adopt(t.Context(), db, second.ID, b); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Adopt(t.Context(), db, first.ID, b); !errors.Is(err, fileownership.ErrOwnerChanged) {
		t.Fatalf("second game acquired same file: %v", err)
	}
	if err := fileownership.RetireAll(t.Context(), db, a, 20); err != nil {
		t.Fatal(err)
	}
	var retired bool
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT retired_at_ms IS NOT NULL FROM stored_files WHERE id=?`, second.ID).Scan(&retired); err != nil || retired {
		t.Fatalf("other game retired: %v %v", retired, err)
	}
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(second.Path); err != nil || string(contents) != "same ROM or cover" {
		t.Fatalf("other game lost file: %s %v", contents, err)
	}
}

func TestHandoffRollsBackAndFormerOwnerCannotRetirePublishedFile(t *testing.T) {
	db, files := ownerFixture(t)
	file := registerFile(t, db, files)
	item := fileownership.Owner{Kind: "IMPORT_ITEM", ID: "item"}
	game := fileownership.Owner{Kind: "GAME", ID: "game"}
	if err := fileownership.Adopt(t.Context(), db, file.ID, item); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Transfer(t.Context(), tx, file.ID, item, game); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Transfer(t.Context(), db, file.ID, item, game); err != nil {
		t.Fatalf("rollback lost item ownership: %v", err)
	}
	if err := fileownership.RetireAll(t.Context(), db, item, 30); err != nil {
		t.Fatal(err)
	}
	var kind, id string
	var retired bool
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT owner_kind,owner_id,retired_at_ms IS NOT NULL FROM stored_files WHERE id=?`, file.ID).Scan(&kind, &id, &retired); err != nil {
		t.Fatal(err)
	}
	if kind != "GAME" || id != "game" || retired {
		t.Fatalf("handoff corrupted: %s %s %v", kind, id, retired)
	}
	if err := fileownership.RetireAll(t.Context(), db, game, 40); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Transfer(t.Context(), db, file.ID, game, item); !errors.Is(err, fileownership.ErrOwnerChanged) {
		t.Fatalf("retired file revived: %v", err)
	}
}

func ownerFixture(t *testing.T) (dbapi.DB, *filestore.Store) {
	t.Helper()
	root := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(root, "retrom.db"), time.Now)
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
	return database.SQL, files
}

func registerFile(t *testing.T, db dbapi.DB, files *filestore.Store) filestore.Metadata {
	t.Helper()
	metadata, err := files.Put(bytes.NewBufferString("same ROM or cover"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := filecatalog.EnsureRecord(t.Context(), db, metadata, "application/octet-stream", 10)
	if err != nil || id != metadata.ID {
		t.Fatalf("register independent file: %s %v", id, err)
	}
	return metadata
}

func TestStagingRetirementOnlyTargetsExpiredUnclaimedFiles(t *testing.T) {
	db, files := ownerFixture(t)
	old := registerFile(t, db, files)
	current := registerFile(t, db, files)
	owned := registerFile(t, db, files)
	if _, err := db.ExecContext(t.Context(), `UPDATE stored_files SET created_at_ms=100 WHERE id=?`, current.ID); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Adopt(t.Context(), db, owned.ID, fileownership.Owner{Kind: "GAME", ID: "game"}); err != nil {
		t.Fatal(err)
	}
	staging := filecatalog.NewStaging(db)
	if err := staging.Retire(t.Context(), 50, 200); err != nil {
		t.Fatal(err)
	}
	for _, file := range []filestore.Metadata{old, current, owned} {
		found, err := staging.Registered(t.Context(), file.ID)
		if err != nil || !found {
			t.Fatalf("catalog lost %s: %v", file.ID, err)
		}
		var retired bool
		if err := dbapi.QueryRowContext(t.Context(), db, `SELECT retired_at_ms IS NOT NULL FROM stored_files WHERE id=?`, file.ID).Scan(&retired); err != nil {
			t.Fatal(err)
		}
		if retired != (file.ID == old.ID) {
			t.Fatalf("wrong retirement %s: %v", file.ID, retired)
		}
	}
	if err := fileownership.Adopt(t.Context(), db, old.ID, fileownership.Owner{Kind: "GAME", ID: "late"}); !errors.Is(err, fileownership.ErrOwnerChanged) {
		t.Fatalf("adopted retired staging file: %v", err)
	}
}

func TestRegistrationRejectsExpiredUnregisteredWrites(t *testing.T) {
	db, files := ownerFixture(t)
	metadata, err := files.Put(bytes.NewBufferString("expired staging"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * filestore.UnregisteredLifetime)
	if err := os.Chtimes(metadata.Path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := filecatalog.EnsureRecord(t.Context(), db, metadata, "application/octet-stream", 10); !errors.Is(err, filestore.ErrStagingExpired) {
		t.Fatalf("expired write registered: %v", err)
	}
	found, err := filecatalog.NewStaging(db).Registered(t.Context(), metadata.ID)
	if err != nil || found {
		t.Fatalf("expired catalog row: %v %v", found, err)
	}
}
