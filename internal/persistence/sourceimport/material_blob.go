package sourceimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/persistence/filecatalog"
	application "retrom/internal/service/sourceimport"
)

func registerVerifiedMaterial(
	ctx context.Context,
	db dbapi.Executor,
	blob application.VerifiedBlob,
	mediaType string,
	now int64,
) (string, error) {
	metadata := filestore.Metadata{
		ID:     blob.ID,
		Path:   blob.StoragePath,
		SHA256: blob.SHA256,
		MD5:    blob.MD5,
		SHA1:   blob.SHA1,
		CRC32:  blob.CRC32,
		Size:   blob.Size,
	}
	blobID, err := filecatalog.EnsureRecord(ctx, db, metadata, mediaType, now)
	if err != nil {
		return "", fmt.Errorf("register Source material blob: %w", err)
	}
	actual := application.VerifiedBlob{ID: blob.ID, StoragePath: blob.StoragePath}
	if err := dbapi.QueryRowContext(
		ctx,
		db,
		`SELECT sha256,md5,sha1,crc32,size_bytes FROM stored_files WHERE id=?`,
		blobID,
	).Scan(
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
