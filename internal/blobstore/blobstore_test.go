package blobstore

import (
	"bytes"
	"os"
	"sync"
	"testing"

	"retrom/internal/testassert"
)

func TestPutDeduplicatesConcurrentContent(t *testing.T) {
	t.Parallel()
	store, err := Open(t.TempDir())
	testassert.Falsef(t, err != nil, "Open() error = %v", err)
	payload := bytes.Repeat([]byte("retrom"), 4096)
	results := make([]Metadata, 2)
	errorsFound := make([]error, 2)
	var wait sync.WaitGroup
	for index := range results {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index], errorsFound[index] = store.Put(bytes.NewReader(payload))
		}()
	}
	wait.Wait()
	testassert.Falsef(t, testassert.Any(func() bool { return errorsFound[0] != nil }, func() bool { return errorsFound[1] != nil }), "Put() errors = %v, %v", errorsFound[0], errorsFound[1])
	testassert.False(t, testassert.Any(func() bool { return results[0].SHA256 != results[1].SHA256 }, func() bool { return results[0].Path != results[1].Path }), "equal content did not converge to one CAS path")
	testassert.Falsef(t, results[0].Existing == results[1].Existing, "Existing flags = %v/%v, want one publisher", results[0].Existing, results[1].Existing)
	contents, err := os.ReadFile(results[0].Path)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !bytes.Equal(contents, payload) }), "published content mismatch: %v", err)
}

func TestCandidatePublishesOnlyWhenCommitted(t *testing.T) {
	t.Parallel()
	store, err := Open(t.TempDir())
	testassert.Falsef(t, err != nil, "Open() error = %v", err)
	payload := []byte("staged archive member")
	candidate, err := store.Stage(bytes.NewReader(payload))
	testassert.Falsef(t, err != nil, "Stage() error = %v", err)
	metadata := candidate.Metadata()
	_, statErr := os.Stat(store.Path(metadata.SHA256))
	testassert.Truef(t, os.IsNotExist(statErr), "staged candidate was published before commit: %v", statErr)
	published, err := candidate.Commit()
	testassert.Falsef(t, err != nil, "Commit() error = %v", err)
	contents, err := os.ReadFile(published.Path)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return !bytes.Equal(contents, payload) },
	), "committed candidate mismatch: %v", err)
	testassert.Falsef(t, candidate.Discard() != nil, "discarding a committed candidate is not idempotent")

	discarded, err := store.Stage(bytes.NewReader([]byte("unused archive sidecar")))
	testassert.Falsef(t, err != nil, "Stage() for discard error = %v", err)
	discardedMetadata := discarded.Metadata()
	testassert.Falsef(t, discarded.Discard() != nil, "Discard() error")
	_, statErr = os.Stat(store.Path(discardedMetadata.SHA256))
	testassert.Truef(t, os.IsNotExist(statErr), "discarded candidate reached CAS: %v", statErr)
}
