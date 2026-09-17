package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/adapter/files/blobstore"
	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/dbexec"
)

func registerVerifiedMaterial(
	ctx context.Context,
	db dbexec.Executor,
	blob application.VerifiedBlob,
	mediaType string,
	now int64,
) (string, error) {
	metadata := blobstore.Metadata{SHA256: blob.SHA256, MD5: blob.MD5, SHA1: blob.SHA1, CRC32: blob.CRC32, Size: blob.Size}
	blobID, err := blobcatalog.EnsureRecord(ctx, db, metadata, mediaType, now)
	if err != nil {
		return "", fmt.Errorf("register Pegasus material blob: %w", err)
	}
	var actual application.VerifiedBlob
	if err := db.QueryRowContext(ctx, `SELECT sha256,md5,sha1,crc32,size_bytes FROM blobs WHERE id=?`, blobID).Scan(
		&actual.SHA256, &actual.MD5, &actual.SHA1, &actual.CRC32, &actual.Size,
	); err != nil {
		return "", fmt.Errorf("verify Pegasus catalog facts: %w", err)
	}
	if actual != blob {
		return "", application.ErrVersionConflict
	}
	return blobID, nil
}
