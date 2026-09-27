package filedeletion

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"retrom/internal/filestore"
)

func TestDeletionRetryCannotRemoveAnotherFileWithIdenticalBytes(t *testing.T) {
	store, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.Put(bytes.NewBufferString("same bytes"))
	if err != nil {
		t.Fatal(err)
	}
	files := New(store)
	if err := files.Delete(t.Context(), original.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := store.Put(bytes.NewBufferString("same bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == original.ID || replacement.SHA256 != original.SHA256 {
		t.Fatal("file identity is coupled to hash")
	}
	if err := files.Delete(t.Context(), original.ID); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(replacement.Path)
	if err != nil || string(contents) != "same bytes" {
		t.Fatalf("retry removed replacement: %s %v", contents, err)
	}
}

func TestDeleteRejectsNilStoreAndCanceledContext(t *testing.T) {
	if err := New(nil).Delete(t.Context(), "invalid"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nil store error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := New(nil).Delete(ctx, "invalid"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error: %v", err)
	}
	store, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := New(store).Delete(t.Context(), "../../outside"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("unsafe ID: %v", err)
	}
}

func TestWaitHonorsCancellationAndDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := New(nil).Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait: %v", err)
	}
	if err := New(nil).Wait(t.Context(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
