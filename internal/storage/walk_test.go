package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerRetainsOffsetsAndBoundsEveryBatch(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	files := filepath.Join(directory, "games", "00000000-0000-4000-8000-000000000001")
	if err = os.MkdirAll(files, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2500; i++ {
		if err = os.WriteFile(filepath.Join(files, fmt.Sprintf("%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scanner := store.Scanner()
	defer scanner.Close()
	seen := make(map[string]bool)
	batches := 0
	for {
		entries, finished, readErr := scanner.Next(100)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) > 100 {
			t.Fatalf("unbounded batch %d", len(entries))
		}
		for _, entry := range entries {
			if seen[entry.Key] {
				t.Fatalf("duplicate %s", entry.Key)
			}
			seen[entry.Key] = true
		}
		batches++
		if finished {
			break
		}
		if batches > 30 {
			t.Fatal("scanner failed to complete")
		}
	}
	if len(seen) != 2500 || batches < 25 {
		t.Fatalf("files=%d batches=%d", len(seen), batches)
	}
}

func TestScannerHandlesDirectoryRemovedWhileOpen(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	}()
	owner := filepath.Join(directory, "games", "00000000-0000-4000-8000-000000000001")
	if err = os.MkdirAll(owner, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(owner, "game.nes"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := store.Scanner()
	defer scanner.Close()
	if _, _, err = scanner.Next(2); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(owner); err != nil {
		t.Fatal(err)
	}
	_, finished, err := scanner.Next(100)
	if err != nil || !finished {
		t.Fatalf("finished=%v error=%v", finished, err)
	}
}
