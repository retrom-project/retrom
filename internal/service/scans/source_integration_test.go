//go:build integration

package scans

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
	"retrom/internal/testsupport"
)

func TestBiosSelectedAbsoluteSymlinkCopiesManagedBytesAndProgress(t *testing.T) {
	t.Parallel()
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
	directory := t.TempDir()
	payload := []byte("identified BIOS candidate bytes")
	if err = os.WriteFile(filepath.Join(directory, "firmware.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "selected-bios")
	if err = os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	s := &Service{Repository: f.Repository, Storage: store, Now: time.Now}
	root, err := s.Sources.Open(link)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	digest := sha256.Sum256(payload)
	prepared, err := s.prepareBios(t.Context(), root,
		runtimeclient.BiosRequirement{RequirementKey: "test/firmware.bin"},
		[]biosCandidate{{Name: "firmware.bin", SHA256: hex.EncodeToString(digest[:])}})
	if err != nil {
		t.Fatal(err)
	}
	scan := model.Scan{ID: uuid.NewString(), ScanType: "bios", Status: "running", TotalKnown: true, TotalCount: 1, CreatedAtMs: 1000}
	if err = f.Repository.CreateScan(t.Context(), scan, f.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.commitBios(t.Context(), &scan, prepared, nil); err != nil {
		t.Fatal(err)
	}
	assertCounts(t, f.Repository, scan.ID, 1, 1)
	installed, err := f.Repository.Bios(t.Context(), prepared.RequirementKey)
	if err != nil || installed.ID != prepared.ID {
		t.Fatalf("installed=%+v error=%v", installed, err)
	}
	if err = os.WriteFile(filepath.Join(directory, "firmware.bin"), []byte("source changes after import"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStoredBytes(t, store, installed.StorageKey, payload)
}
