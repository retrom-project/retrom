//go:build integration

package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
	"retrom/internal/testsupport"
)

func TestRemovalFailureKeepsDeletedRowAndCurrentReferenceProtectsBytes(t *testing.T) {
	r := testsupport.Database(t)
	directory := t.TempDir()
	store, err := storage.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Unix(200000, 0)
	service := &Service{Repository: r, Storage: store, Now: func() time.Time { return now }, Grace: time.Hour}
	first := failedRemovalFixture(t, r, directory)
	old := sharedActiveReferenceFixture(t, r, directory)
	if err = service.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertRetiredRemovalAndActiveBytes(t, r, directory, first, old)
	if service.scanner != nil {
		service.scanner.Close()
	}
}

func failedRemovalFixture(t *testing.T, r *persistence.Repository, directory string) model.BiosFile {
	t.Helper()
	blocked := "bios/" + uuid.NewString() + "/nonempty"
	if err := os.MkdirAll(filepath.Join(directory, blocked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, blocked, "child"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := model.BiosFile{ID: uuid.NewString(), RequirementKey: "firmware/blocked.bin", Filename: "blocked.bin", StorageKey: blocked, SHA256: "blocked", SizeBytes: 8}
	if err := r.WriteBios(t.Context(), first, 1000); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteBios(t.Context(), first.RequirementKey, 2000); err != nil {
		t.Fatal(err)
	}
	return first
}

func sharedActiveReferenceFixture(t *testing.T, r *persistence.Repository, directory string) model.BiosFile {
	t.Helper()
	shared := "bios/" + uuid.NewString() + "/shared"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(directory, shared)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, shared), []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(directory, shared), time.Unix(0, 0), time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	old := model.BiosFile{ID: uuid.NewString(), RequirementKey: "firmware/shared.bin", Filename: "shared.bin", StorageKey: shared, SHA256: "shared", SizeBytes: 7}
	active := old
	active.ID = uuid.NewString()
	if err := r.Transaction(t.Context(), func(tx *persistence.Repository) error {
		if err := tx.WriteBios(t.Context(), old, 1000); err != nil {
			return err
		}
		return tx.WriteBios(t.Context(), active, 2000)
	}); err != nil {
		t.Fatal(err)
	}
	return old
}

func assertRetiredRemovalAndActiveBytes(t *testing.T, r *persistence.Repository, directory string, first, old model.BiosFile) {
	t.Helper()
	if count, err := r.Count(t.Context(), "SELECT count(*) FROM bios_file_tab WHERE id=$1 AND status='deleted'", first.ID); err != nil || count != 1 {
		t.Fatalf("failed removal row=%d error=%v", count, err)
	}
	if count, err := r.Count(t.Context(), "SELECT count(*) FROM bios_file_tab WHERE id=$1 AND status='purged'", old.ID); err != nil || count != 1 {
		t.Fatalf("retired shared row=%d error=%v", count, err)
	}
	bytes, err := os.ReadFile(filepath.Join(directory, old.StorageKey))
	if err != nil || string(bytes) != "current" {
		t.Fatalf("active bytes=%q error=%v", bytes, err)
	}
	bytes, err = os.ReadFile(filepath.Join(directory, first.StorageKey, "child"))
	if err != nil || string(bytes) != "preserve" {
		t.Fatalf("failed-removal bytes=%q error=%v", bytes, err)
	}
}
