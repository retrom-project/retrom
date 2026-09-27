package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUncommittedSweepKeepsDurableAndRecentPreparations(t *testing.T) {
	root := t.TempDir()
	files, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * ScratchLifetime)
	durable, abandoned, recent := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{durable, abandoned, recent} {
		directory := filepath.Join(root, ItemDirectory(id))
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if id != recent {
			if err := os.Chtimes(directory, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	lookupFailure := errors.New("database unavailable")
	if err := files.SweepUncommitted(t.Context(), time.Now().Add(-ScratchLifetime), func(context.Context, string, string) (bool, error) { return false, lookupFailure }); !errors.Is(err, lookupFailure) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ItemDirectory(abandoned))); err != nil {
		t.Fatal("lookup failure removed preparation", err)
	}
	err = files.SweepUncommitted(t.Context(), time.Now().Add(-ScratchLifetime), func(_ context.Context, kind, id string) (bool, error) {
		if kind != "items" {
			t.Fatalf("unexpected lookup: %s", kind)
		}
		return id == durable, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{durable, recent} {
		if _, err := os.Stat(filepath.Join(root, ItemDirectory(id))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ItemDirectory(abandoned))); !os.IsNotExist(err) {
		t.Fatal("abandoned preparation remains", err)
	}
}
