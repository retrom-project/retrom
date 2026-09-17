package launch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/launch"
)

var (
	errLockedBlobSizeMismatch = errors.New("locked blob size mismatch")
	errLockedPlaylistMismatch = errors.New("locked playlist mismatch")
)

type productBlobVerifier struct{ blobs *blobstore.Store }

func (verifier productBlobVerifier) Verify(ctx context.Context, check application.ProductBlobCheck) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("verify product blob: %w", err)
	}
	if verifier.blobs == nil {
		return nil
	}
	file, err := verifier.blobs.OpenDigest(check.Digest)
	if err != nil {
		return fmt.Errorf("open locked blob: %w", err)
	}
	defer func() { cleanup.Error("close product blob", file.Close()) }()
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect locked blob: %w", err)
	}
	if stat.Size() != check.SizeBytes {
		return errLockedBlobSizeMismatch
	}
	if check.Exact == nil {
		return nil
	}
	actual, err := io.ReadAll(io.LimitReader(file, int64(len(check.Exact))+1))
	if err != nil {
		return fmt.Errorf("read locked playlist: %w", err)
	}
	if !bytes.Equal(actual, check.Exact) {
		return errLockedPlaylistMismatch
	}
	return nil
}
