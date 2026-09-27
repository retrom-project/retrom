// Package filedeletion provides the filesystem side of payload lifecycle
// operations. It is kept separate from the application service and database
// repositories so the release workflow can be composed with another storage
// implementation later.
package filedeletion

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/filestore"
)

// Store adapts independent file storage to deletion and cleanup wait ports.
type Store struct{ blobs *filestore.Store }

func New(blobs *filestore.Store) *Store { return &Store{blobs: blobs} }

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
