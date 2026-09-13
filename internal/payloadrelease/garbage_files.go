package payloadrelease

import (
	"context"
	"errors"
	"fmt"
	"os"

	"retrom/internal/blobstore"
	application "retrom/internal/service/payloadrelease"
)

type garbageFiles struct{ blobs *blobstore.Store }

func (files garbageFiles) Delete(ctx context.Context, digest string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop garbage file removal: %w", err)
	}
	if files.blobs == nil {
		return application.ErrInputInvalid
	}
	if err := os.Remove(files.blobs.Path(digest)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove garbage file: %w", err)
	}
	return nil
}
