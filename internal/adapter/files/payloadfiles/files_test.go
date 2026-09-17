package payloadfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	payloadservice "retrom/internal/model/payloadrelease"
)

func TestDeleteRemovesBlobAndTreatsMissingAsSuccess(t *testing.T) {
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	path := store.Path(digest)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := New(store)
	if err := files.Delete(t.Context(), digest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blob still exists: %v", err)
	}
	if err := files.Delete(t.Context(), digest); err != nil {
		t.Fatalf("missing blob should be successful: %v", err)
	}
}

func TestDeleteRejectsNilStoreAndCanceledContext(t *testing.T) {
	if err := New(nil).Delete(t.Context(), strings.Repeat("a", 64)); !errors.Is(err, payloadservice.ErrInputInvalid) {
		t.Fatalf("nil store error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := New(nil).Delete(ctx, strings.Repeat("a", 64)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delete error = %v", err)
	}
}

func TestWaitHonorsCancellationAndDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := New(nil).Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait error = %v", err)
	}
	started := time.Now()
	if err := New(nil).Wait(t.Context(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < time.Millisecond {
		t.Fatal("wait returned before its delay")
	}
}
