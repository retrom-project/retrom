package filestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SweepUncommitted removes abandoned preparation directories. A durable import,
// upload or source item protects its directory regardless of the review's age.
func (store *Store) SweepUncommitted(ctx context.Context, cutoff time.Time,
	inUse func(context.Context, string, string) (bool, error),
) error {
	for _, kind := range []string{"items", "uploads", "sources"} {
		if err := store.sweepUncommittedKind(ctx, kind, cutoff, inUse); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) sweepUncommittedKind(ctx context.Context, kind string, cutoff time.Time,
	inUse func(context.Context, string, string) (bool, error),
) error {
	directory := filepath.Join(store.root, "staging", kind)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read preparation directories: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validObjectID(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect preparation directory: %w", err)
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		active, err := inUse(ctx, kind, entry.Name())
		if err != nil {
			return fmt.Errorf("read preparation owner: %w", err)
		}
		if active {
			continue
		}
		if err := store.RemovePath(ctx, "staging/"+kind+"/"+entry.Name()); err != nil {
			return err
		}
	}
	return nil
}
