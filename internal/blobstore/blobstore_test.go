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
