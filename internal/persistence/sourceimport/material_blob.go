package sourceimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	application "retrom/internal/service/sourceimport"
)

func registerVerifiedMaterial(
	_ context.Context,
	_ dbapi.Executor,
	blob application.VerifiedBlob,
	mediaType string,
	_ int64,
) (string, error) {
	metadata := filestore.Metadata{
		Record: blob.ID,
		SHA256: blob.SHA256,
		MD5:    blob.MD5,
		SHA1:   blob.SHA1,
		CRC32:  blob.CRC32,
		Size:   blob.Size,
	}
	fileRecord, err := filestore.FileRecord(metadata, mediaType)
	if err != nil {
		return "", fmt.Errorf("register Source material blob: %w", err)
	}
	return fileRecord, nil
}
