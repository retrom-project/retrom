package filestore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
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
	copied, err := store.Copy(t.Context(), first.Record)
	if err != nil {
		t.Fatal(err)
	}
	if first.Record == copied.Record || first.SHA256 != copied.SHA256 || first.Size != copied.Size {
		t.Fatalf("copy=%+v first=%+v", copied, first)
	}
	if value, err := os.ReadFile(copied.Path); err != nil || string(value) != "owned copy" {
		t.Fatalf("bytes=%q error=%v", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Copy(ctx, first.Record); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}
