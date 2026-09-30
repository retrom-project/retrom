package filestore

import (
	"strings"
	"testing"
)

func TestPreviewCheckpointCleanupPreservesNewCheckpoint(t *testing.T) {
	t.Parallel()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input, err := store.Put(strings.NewReader("checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	preview := "previews/018fbe68-0000-7000-8000-000000000001/checkpoints/"
	older, err := store.CopyTo(t.Context(), input.Record, preview+"018fbe68-0000-7000-8000-000000000002", "payload")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := store.CopyTo(t.Context(), input.Record, preview+"018fbe68-0000-7000-8000-000000000003", "payload")
	if err != nil {
		t.Fatal(err)
	}
	retired, err := CleanupDirectory(older.Record)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePath(t.Context(), retired); err != nil {
		t.Fatal(err)
	}
	file, err := store.OpenRecord(newer.Record)
	if err != nil {
		t.Fatalf("old checkpoint cleanup removed current checkpoint: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
