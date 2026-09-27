// Package payloadfiles provides the filesystem side of payload lifecycle
// operations. It is kept separate from the application service and database
// repositories so the release workflow can be composed with another storage
// implementation later.
package payloadfiles

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/blobstore"
)

// Store adapts the blob store to payload release's file and wait ports.
type Store struct{ blobs *blobstore.Store }

func New(blobs *blobstore.Store) *Store { return &Store{blobs: blobs} }

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
