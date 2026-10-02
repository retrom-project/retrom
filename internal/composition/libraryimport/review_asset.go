package libraryimport

import (
	"context"
	"fmt"
	"io"
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/filestore"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewAssetUploads(
	database dbapi.DB, blobs *filestore.Store, now func() time.Time,
) *libraryservice.ReviewAssetUploads {
	return libraryservice.NewReviewAssetUploads(repository.NewReviewAssetUploads(database), reviewAssetBlobs{blobs}, now)
}

type reviewAssetBlobs struct{ store *filestore.Store }

func (blobs reviewAssetBlobs) OpenRecord(digest string) (io.ReadCloser, error) {
	file, err := blobs.store.OpenRecord(digest)
	if err != nil {
		return nil, fmt.Errorf("open review asset blob: %w", err)
	}
	return file, nil
}

func (blobs reviewAssetBlobs) CopyTo(ctx context.Context, value, directory, name string) (filestore.Metadata, error) {
	file, err := blobs.store.CopyTo(ctx, value, directory, name)
	if err != nil {
		return filestore.Metadata{}, fmt.Errorf("prepare review asset: %w", err)
	}
	return file, nil
}
