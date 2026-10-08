package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFailedWriteAndRenameCleanAllocatedFilesAndPreserveExistingContent(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	id := uuid.NewString()
	existing, err := store.Write(t.Context(), "games", id, strings.NewReader("existing"), 20)
	if err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("source interrupted")
	readers := []io.Reader{
		&writeHookReader{Reader: strings.NewReader("new"), hook: func() error { return readErr }},
		&writeHookReader{Reader: strings.NewReader("new"), hook: func() error {
			files, err := os.ReadDir(filepath.Join(directory, "temporary"))
			if err != nil {
				return err
			}
			if len(files) != 1 {
				t.Fatalf("temporary allocations=%d", len(files))
			}
			// Renaming a file over a directory fails after the complete temporary write.
			return os.Mkdir(filepath.Join(directory, "games", id, files[0].Name()), 0o700)
		}},
	}
	for _, reader := range readers {
		if _, err = store.Write(t.Context(), "games", id, reader, 20); err == nil {
			t.Fatal("expected failed file preparation")
		}
		allocated, err := os.ReadDir(filepath.Join(directory, "temporary"))
		if err != nil || len(allocated) != 0 {
			t.Fatalf("failed write retained temporary bytes: count=%d error=%v", len(allocated), err)
		}
		owned, err := os.ReadDir(filepath.Join(directory, "games", id))
		if err != nil || len(owned) != 1 {
			t.Fatalf("failed write changed existing owner: count=%d error=%v", len(owned), err)
		}
		data, err := os.ReadFile(filepath.Join(directory, existing.Key))
		if err != nil || string(data) != "existing" {
			t.Fatalf("existing file changed: %q error=%v", data, err)
		}
	}
}

type writeHookReader struct {
	io.Reader
	hook func() error
}

func (r *writeHookReader) Read(p []byte) (int, error) {
	if r.hook != nil {
		hook := r.hook
		r.hook = nil
		if err := hook(); err != nil {
			return 0, err
		}
	}
	return r.Reader.Read(p)
}
