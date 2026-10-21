//go:build integration

package scans

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestMameCompanionCopyUsesOnlyTheMetadataDirectory(t *testing.T) {
	for _, inside := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside only", true: "sibling directory"}[inside], func(t *testing.T) {
			f := testsupport.Library(t)
			directory := t.TempDir()
			store, client := mameScanRuntime(t)
			fixtureMameSource(t, directory, inside)
			root, err := os.OpenRoot(filepath.Join(directory, "collection"))
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot(root)
			service := &Service{Repository: f.Repository, Runtime: client, Storage: store, Now: time.Now}
			selected := model.Directory{PlatformID: "arcade", DefaultCoreID: "mame_arcade", CoreIDs: []string{"mame_arcade"}}
			files, err := service.content(t.Context(), root, f.Game.ID, []string{"A/shikigam.zip"}, selected, &sourceInventory{})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if inside {
				want = 2
			}
			if len(files) != want {
				t.Fatalf("files=%+v", files)
			}
			f.Game.Files, err = service.finalizeContent(t.Context(), f.Game.ID, model.Directory{PlatformID: "arcade", DefaultCoreID: "mame_arcade"}, json.RawMessage(`{"content":{"kind":"ARCADE","entryFile":"shikigam.zip"}}`), files)
			if err != nil {
				t.Fatal(err)
			}
			f.Game.Input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"ARCADE","entryFile":"shikigam.zip"}}`)
			scan := model.Scan{ID: uuid.NewString(), ScanType: "game", Status: "running", TotalKnown: true, TotalCount: 1, CreatedAtMs: 1000}
			if err = f.Repository.CreateScan(t.Context(), scan, f.Principal.User.ID); err != nil {
				t.Fatal(err)
			}
			if err = service.commitGame(t.Context(), f.Principal, f.Game, &scan); err != nil {
				t.Fatal(err)
			}
			assertCounts(t, f.Repository, scan.ID, 1, 1)
			if inside {
				assertMameCopy(t, store, model.FileContent(f.Game.Files[1]), directory)
			}
		})
	}
}

func fixtureMameSource(t *testing.T, directory string, inside bool) {
	t.Helper()
	for _, folder := range []string{"collection/A", "collection/B"} {
		if err := os.MkdirAll(filepath.Join(directory, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "collection/A/shikigam.zip"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 4096)
	copy(header, "MComprHD")
	binary.BigEndian.PutUint32(header[8:], 124)
	binary.BigEndian.PutUint32(header[12:], 5)
	binary.BigEndian.PutUint64(header[32:], 4096)
	binary.BigEndian.PutUint64(header[40:], 124)
	binary.BigEndian.PutUint32(header[56:], 4096)
	binary.BigEndian.PutUint32(header[60:], 512)
	hash, err := hex.DecodeString("fa49a0bc47f5cb7c30d7e49e2c3696b21bafb840")
	if err != nil {
		t.Fatal(err)
	}
	copy(header[84:], hash)
	if err = os.WriteFile(filepath.Join(directory, "outside.chd"), header, 0o600); err != nil {
		t.Fatal(err)
	}
	if inside {
		if err = os.WriteFile(filepath.Join(directory, "collection/B/renamed.chd"), header, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertMameCopy(t *testing.T, store *storage.Store, file model.ContentFile, directory string) {
	t.Helper()
	if file.LogicalKey != "shikigam/shikigam.chd" || file.SizeBytes != 4096 {
		t.Fatalf("companion=%+v", file)
	}
	original, err := os.ReadFile(filepath.Join(directory, "collection/B/renamed.chd"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "collection/B/renamed.chd"), []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStoredBytes(t, store, file.StorageKey, original)
}

func mameScanRuntime(t *testing.T) (*storage.Store, *runtimeclient.Client) {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	node := "node"
	if nodeHome := os.Getenv("NODE_HOME"); nodeHome != "" {
		node = filepath.Join(nodeHome, "bin", "node")
	}
	client, err := runtimeclient.Open(t.Context(), os.Getenv("RETROM_RUNTIME_TOOL_INPUT"), node, os.Getenv("RETROM_PROVIDER_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	client.Locate = store.Absolute
	return store, client
}
