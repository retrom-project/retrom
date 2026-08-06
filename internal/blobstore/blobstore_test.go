package blobstore

import (
	"bytes"
	"os"
	"sync"
	"testing"
)

func TestPutDeduplicatesConcurrentContent(t *testing.T) {
	t.Parallel()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
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
	if errorsFound[0] != nil || errorsFound[1] != nil {
		t.Fatalf("Put() errors = %v, %v", errorsFound[0], errorsFound[1])
	}
	if results[0].SHA256 != results[1].SHA256 || results[0].Path != results[1].Path {
		t.Fatal("equal content did not converge to one CAS path")
	}
	if results[0].Existing == results[1].Existing {
		t.Fatalf("Existing flags = %v/%v, want one publisher", results[0].Existing, results[1].Existing)
	}
	contents, err := os.ReadFile(results[0].Path)
	if err != nil || !bytes.Equal(contents, payload) {
		t.Fatalf("published content mismatch: %v", err)
	}
}
