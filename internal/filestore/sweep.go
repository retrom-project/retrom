package filestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Preparation writes must be placed in their domain workspace before this
// deadline. Pending reviews live outside this scratch directory.
const ScratchLifetime = 24 * time.Hour

func (store *Store) SweepWrites(ctx context.Context, cutoff time.Time) error {
	entries, err := os.ReadDir(store.tmp)
	if err != nil {
		return fmt.Errorf("read scratch writes: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sweep interrupted: %w", err)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect scratch write: %w", err)
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(store.tmp, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove abandoned scratch write: %w", err)
		}
	}
	return syncDirectory(store.tmp)
}
