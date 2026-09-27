package filestore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UnregisteredLifetime exceeds every ingestion execution budget (at most 8h).
// A new catalog registration must finish within this window.
const UnregisteredLifetime = 24 * time.Hour

var ErrStagingExpired = errors.New("FILE_STAGING_EXPIRED")

type registrationReader func(context.Context, string) (bool, error)

// Sweep removes abandoned writes. Registered file lifetimes belong to domains.
func (store *Store) Sweep(
	ctx context.Context,
	cutoff time.Time,
	registered func(context.Context, string) (bool, error),
) error {
	for _, root := range []string{store.root, store.tmp} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk staged files: %w", walkErr)
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("sweep interrupted: %w", err)
			}
			if entry.IsDir() {
				return nil
			}
			return store.sweepFile(ctx, root, path, entry, cutoff, registered)
		})
		if err != nil {
			return fmt.Errorf("sweep abandoned writes: %w", err)
		}
	}
	return nil
}

func (store *Store) sweepFile(
	ctx context.Context,
	root, path string,
	entry fs.DirEntry,
	cutoff time.Time,
	registered registrationReader,
) error {
	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("inspect staged file: %w", err)
	}
	if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
		return nil
	}
	removable, err := store.abandoned(ctx, root, path, entry.Name(), registered)
	if err != nil || !removable {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove abandoned file: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func (store *Store) abandoned(
	ctx context.Context,
	root, path, name string,
	registered registrationReader,
) (bool, error) {
	if root != store.root {
		return strings.HasPrefix(name, ".file-"), nil
	}
	if store.Path(name) != path {
		return false, nil
	}
	keep, err := registered(ctx, name)
	if err != nil {
		return false, fmt.Errorf("check file registration: %w", err)
	}
	return !keep, nil
}
