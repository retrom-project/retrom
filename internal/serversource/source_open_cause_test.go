package serversource

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestSelectedDirectoryRetainsFilesystemFailureCause(t *testing.T) {
	for _, stage := range []string{"root", "selected"} {
		t.Run(stage, func(t *testing.T) {
			root, selected := t.TempDir(), ""
			expected := ErrRootUnavailable
			if stage == "root" {
				root = filepath.Join(root, "missing")
			} else {
				selected = "missing"
				expected = ErrPathInvalid
			}
			directory, err := OpenSelectedDirectory(root, selected)
			if directory != nil {
				if closeErr := directory.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if !errors.Is(err, expected) || !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("lost directory cause: %v", err)
			}
		})
	}
}
