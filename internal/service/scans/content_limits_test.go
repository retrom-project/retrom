package scans

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
)

func TestArchiveGameFileCountAdmission(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 10001, 50000, 50001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			readErr := errors.New("source read reached after file count admission")
			opened := false
			entries := make([]entry, count)
			if count > 0 {
				entries[0] = entry{name: "index.html", open: func() (io.ReadCloser, error) {
					opened = true
					return nil, readErr
				}}
			}
			// Stop at the first source read; no managed writes are needed to check admission.
			_, err := (&Service{}).extract(t.Context(), "unused", entries)
			if count == 0 || count > 50000 {
				if !errors.Is(err, model.ErrInvalid) || opened {
					t.Fatalf("count=%d error=%v opened=%v", count, err, opened)
				}
			} else if !errors.Is(err, readErr) || !opened {
				t.Fatalf("allowed file count %d rejected before source read: %v", count, err)
			}
		})
	}
}

func TestGameSourceDirectoryFileLimit(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for index := range 50000 {
		name := filepath.Join(directory, fmt.Sprintf("asset-%05d.bin", index))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	entries, err := readDir(root, ".")
	if err != nil || len(entries) != 50000 {
		t.Fatalf("50000 source files: count=%d error=%v", len(entries), err)
	}
	if err = os.WriteFile(filepath.Join(directory, "overflow.bin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = readDir(root, "."); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("50001 source files: error=%v", err)
	}
}
