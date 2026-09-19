package blobstore

import (
	"bytes"
	"os"
	"testing"

	blobmodel "retrom/internal/model/blob"
)

func TestPreparedContentRetainsVerifiedFactsAcrossPublication(t *testing.T) {
	t.Parallel()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("retrom verified content\n\x00\xff")
	expected := blobmodel.PreparedBlob{
		SHA256: "7abd5b8873d5138f9d4ada2834fa265c92253804484efa2c5d1d2c2084968bc0", MD5: "6dad663d79aec66eced95b909945ba4c", SHA1: "9dda6620f445a9afe1f2afa209c22b02f4ebffb8", CRC32: "f458fffa", Size: 26,
	}
	candidate, err := store.Stage(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := candidate.Discard(); err != nil {
			t.Error(err)
		}
	})
	stagedPath := candidate.Path()
	if candidate.Metadata() != expected {
		t.Fatalf("staged facts: %+v", candidate.Metadata())
	}
	staged, err := os.ReadFile(stagedPath)
	if err != nil || !bytes.Equal(staged, contents) {
		t.Fatalf("private staged bytes: %q %v", staged, err)
	}
	if _, err := os.Stat(store.Path(expected.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("premature publication: %v", err)
	}
	actual, err := candidate.Commit()
	if err != nil || actual != expected {
		t.Fatalf("published facts: %+v %v", actual, err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("staging retained after commit: %v", err)
	}
	again, err := store.Put(bytes.NewReader(contents))
	if err != nil || again != expected {
		t.Fatalf("deduplicated facts: %+v %v", again, err)
	}
	published, err := os.ReadFile(store.Path(actual.SHA256))
	if err != nil || !bytes.Equal(published, contents) {
		t.Fatalf("published bytes: %q %v", published, err)
	}
}
