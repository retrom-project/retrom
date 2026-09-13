package uploadfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadCleanupHonorsCancellationAndPreservesUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	source := New(root)
	directory := filepath.Join(root, "tmp", "uploads", "owner", "file")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "0-part")
	if err := os.WriteFile(file, []byte("part"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := source.Remove(ctx, "owner", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup ignored cancellation: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("cancelled cleanup removed bytes: %v", err)
	}
	sibling := filepath.Join(root, "tmp", "uploads", "sibling")
	if err := os.MkdirAll(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := source.Remove(t.Context(), "owner", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned bytes retained: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("unrelated upload removed: %v", err)
	}
}
