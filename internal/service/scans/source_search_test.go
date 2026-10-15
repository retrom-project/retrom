package scans

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMetadataSourceInventoryIsBoundedAndReusable(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "collection", "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outside.chd", "collection/other/renamed.chd", "collection/game.zip"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("header-content"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(directory, "outside.chd"), filepath.Join(directory, "collection", "escape.chd")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Join(directory, "collection"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	inventory := &sourceInventory{}
	query := sourceSearch{Extensions: []string{"chd"}, HeaderBytes: 6}
	files, err := inventory.search(t.Context(), root, query)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%+v error=%v", files, err)
	}
	if files[0].SourcePath != "other/renamed.chd" || files[0].HeaderBase64 != base64.StdEncoding.EncodeToString([]byte("header")) {
		t.Fatalf("unexpected source header: %+v", files[0])
	}
	if err = os.Remove(filepath.Join(directory, "collection", "other", "renamed.chd")); err != nil {
		t.Fatal(err)
	}
	cached, err := inventory.search(t.Context(), root, query)
	if err != nil || len(cached) != 1 {
		t.Fatalf("inventory was unnecessarily rescanned: %+v %v", cached, err)
	}
}

func TestSourceInventorySkipsNamedPipes(t *testing.T) {
	directory := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(directory, "disk.chd"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	files, err := (&sourceInventory{}).search(t.Context(), root, sourceSearch{Extensions: []string{"chd"}, HeaderBytes: 124})
	if err != nil || len(files) != 0 {
		t.Fatalf("special file entered inventory: %+v %v", files, err)
	}
	if _, err = sourceHeader(root, "disk.chd", 124); err == nil {
		t.Fatal("named pipe was accepted as a regular source")
	}
}

func TestRootContentReferences(t *testing.T) {
	for _, path := range []string{"../outside.chd", "/outside.chd", `other\file.chd`} {
		if validContentReference(contentReference{SourcePath: path, LogicalKey: "game/disk.chd"}, nil) {
			t.Fatalf("unsafe root reference accepted: %q", path)
		}
	}
	if !validContentReference(contentReference{SourcePath: "other/renamed.chd", LogicalKey: "game/disk.chd"}, nil) {
		t.Fatal("source and managed logical paths should be independent within the metadata boundary")
	}
}
