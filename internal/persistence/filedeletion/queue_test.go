package filedeletion_test

import (
	"strings"
	"testing"

	"retrom/internal/filestore"
	"retrom/internal/persistence/filedeletion"
)

func TestScratchFilesRemainWithScratchSweeper(t *testing.T) {
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := files.Put(strings.NewReader("uncommitted scratch"))
	if err != nil {
		t.Fatal(err)
	}
	// Scratch has no durable domain owner, so this must not use a database.
	if err := filedeletion.QueueFile(t.Context(), nil, metadata.Record, 1); err != nil {
		t.Fatalf("scratch cleanup must be a successful no-op: %v", err)
	}
}
