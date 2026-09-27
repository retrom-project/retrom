package filestore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestCopyPreservesBytesAndCreatesIndependentIdentity(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(bytes.NewBufferString("owned copy"))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := store.Copy(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == copied.ID || first.SHA256 != copied.SHA256 || first.Size != copied.Size {
		t.Fatalf("copy=%+v first=%+v", copied, first)
	}
	if value, err := os.ReadFile(copied.Path); err != nil || string(value) != "owned copy" {
		t.Fatalf("bytes=%q error=%v", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Copy(ctx, first.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestSweepPreservesRegisteredAndRecentFiles(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-2 * UnregisteredLifetime)
	registered, err := store.Put(bytes.NewBufferString("registered"))
	if err != nil {
		t.Fatal(err)
	}
	abandoned, err := store.Put(bytes.NewBufferString("abandoned"))
	if err != nil {
		t.Fatal(err)
	}
	recent, err := store.Put(bytes.NewBufferString("recent"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []Metadata{registered, abandoned} {
		if err := os.Chtimes(file.Path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	keep := func(_ context.Context, id string) (bool, error) { return id == registered.ID, nil }
	if err := store.Sweep(t.Context(), now.Add(-UnregisteredLifetime), keep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abandoned.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned file remains: %v", err)
	}
	for _, file := range []Metadata{registered, recent} {
		if _, err := os.Stat(file.Path); err != nil {
			t.Fatalf("retained file removed: %v", err)
		}
	}
}

func TestSweepKeepsFileWhenCatalogReadFails(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.Put(bytes.NewBufferString("unconfirmed"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * UnregisteredLifetime)
	if err := os.Chtimes(file.Path, old, old); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("catalog unavailable")
	err = store.Sweep(t.Context(), time.Now().Add(-UnregisteredLifetime), func(context.Context, string) (bool, error) { return false, cause })
	if !errors.Is(err, cause) {
		t.Fatalf("lost cause: %v", err)
	}
	if _, err := os.Stat(file.Path); err != nil {
		t.Fatal(err)
	}
}
