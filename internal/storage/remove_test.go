package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRemovingLastFilePrunesOnlyItsEmptyOwner(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	id := uuid.NewString()
	first, err := store.Write(t.Context(), "bios", id, strings.NewReader("first"), 20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Write(t.Context(), "bios", id, strings.NewReader("current"), 20)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Remove(first.Key); err != nil {
		t.Fatal(err)
	}
	if contents, readErr := os.ReadFile(filepath.Join(directory, second.Key)); readErr != nil || string(contents) != "current" {
		t.Fatalf("nonempty owner was removed: %v", readErr)
	}
	if err = store.Remove(second.Key); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "bios", id)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("empty owner directory remains: %v", statErr)
	}
}
