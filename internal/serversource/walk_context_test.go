package serversource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCancelledWalkStopsAtEmptyDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	visits := 0
	counts, err := WalkFilesContext(ctx, directory, Limits{MaxDepth: 4, MaxDirectories: 8, MaxFiles: 8}, func(File) error { visits++; return ctx.Err() })
	if !errors.Is(err, context.Canceled) || counts.Directories != 0 || visits != 0 {
		t.Fatalf("cancel=%v counts=%#v visits=%d", err, counts, visits)
	}
}

func TestWalkCancellationAfterVisitStopsRemainingEntries(t *testing.T) {
	t.Parallel()
	for _, remaining := range []bool{false, true} {
		t.Run(map[bool]string{false: "last file", true: "remaining entries"}[remaining], func(t *testing.T) {
			t.Parallel()
			assertWalkStopsAfterVisit(t, remaining)
		})
	}
}

func TestWalkContextPreservesVisitorCause(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "game.rom"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})
	failure := errors.New("visitor storage failure")
	counts, err := WalkFilesContext(t.Context(), directory, Limits{MaxDepth: 4, MaxDirectories: 8, MaxFiles: 8}, func(File) error { return failure })
	if !errors.Is(err, failure) || counts.Files != 1 {
		t.Fatalf("visitor=%v counts=%#v", err, counts)
	}
}

func assertWalkStopsAfterVisit(t *testing.T, remaining bool) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.rom"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	if remaining {
		if err := os.MkdirAll(filepath.Join(root, "b-directory", "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "c.rom"), []byte("rom"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	visits := 0
	counts, err := WalkFilesContext(ctx, directory, Limits{MaxDepth: 4, MaxDirectories: 8, MaxFiles: 8}, func(file File) error {
		visits++
		if file.RelativePath != "a.rom" {
			t.Fatalf("visited after cancellation: %s", file.RelativePath)
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || counts.Directories != 1 || counts.Files != 1 || visits != 1 {
		t.Fatalf("cancel=%v counts=%#v visits=%d", err, counts, visits)
	}
}
