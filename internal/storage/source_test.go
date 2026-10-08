package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceDirectoriesBrowseAbsolutePathsAndDirectorySymlinks(t *testing.T) {
	t.Parallel()
	parent, target := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "file"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := Sources{}
	items, err := sources.Directories(parent)
	if err != nil || len(items) != 1 || items[0].Name != "linked" || items[0].Path != link {
		t.Fatalf("directories=%+v error=%v", items, err)
	}
	items, err = sources.Directories(link)
	if err != nil || len(items) != 1 || items[0].Path != filepath.Join(link, "child") {
		t.Fatalf("linked directories=%+v error=%v", items, err)
	}
	root, err := sources.Open(link)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	if info, statErr := root.Stat("child"); statErr != nil || !info.IsDir() {
		t.Fatalf("selected symlink directory unreadable: %v", statErr)
	}
}

func TestSourceDirectoryListingHasNoThousandEntryLimit(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for i := range 1002 {
		if err := os.Mkdir(filepath.Join(directory, fmt.Sprintf("directory-%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	items, err := (Sources{}).Directories(directory)
	if err != nil || len(items) != 1002 || items[0].Name != "directory-0000" || items[1001].Name != "directory-1001" {
		t.Fatalf("count=%d error=%v", len(items), err)
	}
}
