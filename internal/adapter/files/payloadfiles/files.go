// Package payloadfiles provides the filesystem side of payload lifecycle
// operations. It is kept separate from the application service and database
// repositories so the release workflow can be composed with another storage
// implementation later.
package payloadfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"retrom/internal/adapter/files/blobstore"
	payloadservice "retrom/internal/service/payloadrelease"
)

// Store adapts the blob store to payload release's file and wait ports.
type Store struct{ blobs *blobstore.Store }

func New(blobs *blobstore.Store) *Store { return &Store{blobs: blobs} }

func (files *Store) Delete(ctx context.Context, digest string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop garbage file removal: %w", err)
	}
	if files.blobs == nil {
		return payloadservice.ErrInputInvalid
	}
	if err := os.Remove(files.blobs.Path(digest)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove garbage file: %w", err)
	}
	return nil
}

func (*Store) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for payload mutation: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
