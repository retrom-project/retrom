package filedeletion

import (
	"context"
	"fmt"
	"os"
)

func (files *Store) Delete(ctx context.Context, relative string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if files.blobs == nil {
		return os.ErrInvalid
	}
	if err := files.blobs.RemovePath(ctx, relative); err != nil {
		return fmt.Errorf("remove directory: %w", err)
	}
	return nil
}
