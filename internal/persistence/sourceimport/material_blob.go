package sourceimport

import (
	"context"
	"fmt"

	"retrom/internal/blobstore"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobcatalog"
	application "retrom/internal/service/sourceimport"
)

func registerVerifiedMaterial(
	ctx context.Context,
	db dbapi.Executor,
	blob application.VerifiedBlob,
	mediaType string,
	now int64,
) (string, error) {
	metadata := blobstore.Metadata{
		Path:   blob.StoragePath,
		SHA256: blob.SHA256,
		MD5:    blob.MD5,
		SHA1:   blob.SHA1,
		CRC32:  blob.CRC32,
		Size:   blob.Size,
	}
	blobID, err := blobcatalog.EnsureRecord(ctx, db, metadata, mediaType, now)
	if err != nil {
		return "", fmt.Errorf("register Source material blob: %w", err)
	}
	actual := application.VerifiedBlob{StoragePath: blob.StoragePath}
	if err := dbapi.QueryRowContext(ctx, db, `SELECT sha256,md5,sha1,crc32,size_bytes FROM blobs WHERE id=?`, blobID).Scan(

		&actual.SHA256,
		&actual.MD5,
		&actual.SHA1,
		&actual.CRC32,
		&actual.Size,
	); err != nil {
		return "", fmt.Errorf("verify Source catalog facts: %w", err)
	}
	if actual != blob {
		return "", application.ErrVersionConflict
	}
	return blobID, nil
}
