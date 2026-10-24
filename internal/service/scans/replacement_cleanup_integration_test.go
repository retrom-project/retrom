//go:build integration

package scans

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	filemaintenance "retrom/internal/service/files"
	"retrom/internal/storage"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestConfirmedNormalizedReplacementRetiresOnlyOldManagedContent(t *testing.T) {
	s, f, old, current := replacementCleanupFixture(t)
	replacement := f.Game
	replacement.Input.Version = 2
	replacement.Files = []model.GameFile{current}
	if err := f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
		return tx.ReplaceFiles(t.Context(), f.Game.ID, replacement, s.Now().UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.finishReplacement(t.Context(), f.Game.ID, []model.GameFile{current}, persistence.ErrCommitUncertain); err != nil {
		t.Fatalf("lost COMMIT acknowledgment not reconciled: %v", err)
	}
	assertManagedPresent(t, s.Storage, old.StorageKey)
	assertManagedPresent(t, s.Storage, current.StorageKey)
	cleaner := &filemaintenance.Service{
		Repository: f.Repository, Storage: s.Storage, Grace: 24 * time.Hour,
		Now: func() time.Time { return s.Now().Add(25 * time.Hour) },
	}
	if err := cleaner.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	file, err := s.Storage.Read(old.StorageKey)
	if file != nil {
		closeFile(file)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retired original managed copy survived its grace")
	}
	assertManagedPresent(t, s.Storage, current.StorageKey)
	if count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_tab WHERE id=$1 AND status='active'", current.ID); err != nil || count != 1 {
		t.Fatalf("active normalized package=%d err=%v", count, err)
	}
}

func TestRejectedReplacementRemovesNewPackageAndKeepsCurrentContent(t *testing.T) {
	s, f, old, current := replacementCleanupFixture(t)
	if err := s.finishReplacement(t.Context(), f.Game.ID, []model.GameFile{current}, model.ErrConflict); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	assertManagedPresent(t, s.Storage, old.StorageKey)
	file, err := s.Storage.Read(current.StorageKey)
	if file != nil {
		closeFile(file)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uncommitted replacement package survived a known rejection")
	}
}

func TestUnconfirmedReplacementPreservesBothPackagesForReconciliation(t *testing.T) {
	s, f, old, current := replacementCleanupFixture(t)
	// A failed confirmation query must never be interpreted as proof of rollback.
	f.Repository.Close()
	err := s.finishReplacement(t.Context(), f.Game.ID, []model.GameFile{current}, persistence.ErrCommitUncertain)
	if !errors.Is(err, persistence.ErrCommitUncertain) {
		t.Fatalf("commit uncertainty disappeared: %v", err)
	}
	assertManagedPresent(t, s.Storage, old.StorageKey)
	assertManagedPresent(t, s.Storage, current.StorageKey)
}

func replacementCleanupFixture(t *testing.T) (*Service, testsupport.Fixture, model.GameFile, model.GameFile) {
	t.Helper()
	f := testsupport.Library(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	source, err := store.Write(t.Context(), "games", f.Game.ID, strings.NewReader("original managed input"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	old := model.GameFile{
		ID: uuid.NewString(), Role: "content", LogicalKey: "game.nes",
		StorageKey: source.Key, SHA256: source.SHA256, SizeBytes: source.Size,
	}
	f.Game.Files = []model.GameFile{old}
	f.Publish(t)
	staged, err := store.Stage(t.Context(), uuid.NewString(), strings.NewReader("normalized complete bytes"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.BuildPackage(t.Context(), f.Game.ID, []model.ContentFile{{
		LogicalKey: "game.nes", StorageKey: staged.Key, SHA256: staged.SHA256, SizeBytes: staged.Size,
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	return &Service{Repository: f.Repository, Storage: store, Now: func() time.Time { return now }}, f, old, current
}

func assertManagedPresent(t *testing.T, store *storage.Store, key string) {
	t.Helper()
	file, err := store.Read(key)
	if err != nil {
		t.Fatalf("managed content removed unexpectedly: %v", err)
	}
	defer closeFile(file)
	if size, readErr := io.Copy(io.Discard, file); readErr != nil || size == 0 {
		t.Fatalf("managed content unreadable: size=%d err=%v", size, readErr)
	}
}
