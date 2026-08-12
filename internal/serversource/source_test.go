package serversource

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"retrom/internal/cleanup"
)

func TestDeclaredPathNormalization(t *testing.T) {
	t.Parallel()
	value, err := ResolveDeclaredPath("FC/metadata.pegasus.txt", `roms\Metal Max.zip`)
	if err != nil || value != "FC/roms/Metal Max.zip" {
		t.Fatalf("resolved = %q, error=%v", value, err)
	}
	for _, value := range []string{"/absolute", `C:\game.rom`, `\\server\share`, "../escape", "a//b", "https://host/game"} {
		if _, err := NormalizeDeclaredPath(value); !errors.Is(err, ErrPathInvalid) {
			t.Fatalf("NormalizeDeclaredPath(%q) error = %v", value, err)
		}
	}
}

func TestWalkAndOpenStayWithinNoFollowDescriptors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux descriptor contract")
	}
	rootPath := t.TempDir()
	selected := filepath.Join(rootPath, "selected")
	if err := os.MkdirAll(filepath.Join(selected, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selected, "nested", "game.rom"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(rootPath, "outside"), filepath.Join(selected, "escape")); err != nil {
		t.Fatal(err)
	}
	directory, err := OpenSelectedDirectory(rootPath, "selected")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	var found File
	counts, err := WalkFiles(directory, Limits{MaxDepth: 4, MaxDirectories: 8, MaxFiles: 8}, func(file File) error {
		found = file
		return nil
	})
	if err != nil || found.RelativePath != "nested/game.rom" || counts.Files != 1 || counts.SkippedSpecial != 1 {
		t.Fatalf("walk = %#v/%#v, error=%v", found, counts, err)
	}
	handle, before, err := OpenRelativeFile(rootPath, "selected", found.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := handle.Stat()
	cleanup.Error("close", handle.Close())
	if err != nil || !SameFileFacts(before, after) {
		t.Fatalf("file facts error=%v", err)
	}
	if _, _, err := OpenRelativeFile(rootPath, "selected", "escape/file"); err == nil {
		t.Fatal("symlink escape opened")
	}
}
