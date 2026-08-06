package blobstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// EnsureRecord registers a byte-verified CAS object and returns its stable Blob ID.
// The physical write happens before this call so callers can include the reference
// and their domain mutation in one short transaction.
func EnsureRecord(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, metadata Metadata, mediaType string, createdAtMS int64,
) (string, error) {
	var blobID string
	err := executor.QueryRowContext(ctx, `
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
	if err := executor.QueryRowContext(ctx, `
SELECT id
FROM blobs
WHERE sha256=?
`, metadata.SHA256).Scan(&blobID); err != nil {
		return "", fmt.Errorf("read registered blob: %w", err)
	}
	return blobID, nil
}
