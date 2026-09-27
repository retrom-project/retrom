package filedeletion

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/cleanup"
)

// Delete removes only this immutable file ID. Retries cannot address a new file
// with identical bytes because storage identity is independent of its hash.
func (files *Store) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	if files.blobs == nil {
		return os.ErrInvalid
	}
	path := files.blobs.Path(id)
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete owned file: %w", err)
	}
	parent, err := os.Open(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open file directory: %w", err)
	}
	defer func() { cleanup.Error("close file directory", parent.Close()) }()
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("sync file deletion: %w", err)
	}
	return nil
}
