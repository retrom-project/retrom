package blobcatalog

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/blobstore"
	dbapi "retrom/internal/database"

	"github.com/google/uuid"
)

// EnsureRecord registers a byte-verified CAS object and returns its stable Blob ID.
// The physical write happens before this call so callers can include the reference
// and their domain mutation in one short transaction.
func EnsureRecord(
	ctx context.Context, executor dbapi.Executor, metadata blobstore.Metadata, mediaType string, createdAtMS int64,
) (string, error) {
	var id string
	_, err := recordstore.Atomic(
		ctx,
		executor,
		func(tx dbapi.Executor) (sql.Result, error) {
			// Obtain the write reservation before checking CAS presence. GC retires the
			// canonical file under this same database reservation.
			if _, err := tx.ExecContext(ctx, `UPDATE blobs SET id=id WHERE sha256=?`, metadata.SHA256); err != nil {
				return nil, fmt.Errorf("reserve Blob registration: %w", err)
			}
			if metadata.Path != "" {
				info, err := os.Stat(metadata.Path)
				if err != nil {
					return nil, fmt.Errorf("verify published Blob: %w", err)
				}
				if !info.Mode().IsRegular() || info.Size() != metadata.Size {
					return nil, fmt.Errorf("%w: publication changed", os.ErrInvalid)
				}
			}
			var err error
			id, err = ensureRecord(ctx, tx, metadata, mediaType, createdAtMS)
			return driver.RowsAffected(1), err
		},
	)
	if err != nil {
		return "", fmt.Errorf("register verified Blob: %w", err)
	}
	return id, nil
}

func ensureRecord(
	ctx context.Context, executor dbapi.Executor, metadata blobstore.Metadata, mediaType string, createdAtMS int64,
) (string, error) {
	var blobID string
	err := dbapi.QueryRowContext(ctx, executor, `
SELECT id
FROM blobs
WHERE sha256=?
`, metadata.SHA256).Scan(&blobID)
	if err == nil {
		return blobID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find blob record: %w", err)
	}
	generated, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate blob id: %w", err)
	}
	blobID = generated.String()
	_, err = executor.ExecContext(ctx, `
INSERT INTO blobs(id,
sha256,
size_bytes,
md5,
sha1,
crc32,
media_type,
created_at_ms)
VALUES(?,
?,
?,
?,
?,
?,
?,
?) ON CONFLICT(sha256) DO NOTHING
`, blobID, metadata.SHA256, metadata.Size,
		metadata.MD5, metadata.SHA1, metadata.CRC32, mediaType, createdAtMS)
	if err != nil {
		return "", fmt.Errorf("register blob: %w", err)
	}
	if err := dbapi.QueryRowContext(ctx, executor, `
SELECT id
FROM blobs
WHERE sha256=?
`, metadata.SHA256).Scan(&blobID); err != nil {
		return "", fmt.Errorf("read registered blob: %w", err)
	}
	return blobID, nil
}
