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

func NewReviewCoverUploads(
	database dbapi.DB, blobs *filestore.Store, now func() time.Time,
) *libraryservice.ReviewCoverUploads {
	return libraryservice.NewReviewCoverUploads(repository.NewReviewCoverUploads(database), reviewCoverBlobs{blobs}, now)
}

type reviewCoverBlobs struct{ store *filestore.Store }

func (blobs reviewCoverBlobs) OpenRecord(digest string) (io.ReadCloser, error) {
	file, err := blobs.store.OpenRecord(digest)
	if err != nil {
		return nil, fmt.Errorf("open review cover blob: %w", err)
	}
	return file, nil
}

func (blobs reviewCoverBlobs) CopyTo(ctx context.Context, value, directory, name string) (filestore.Metadata, error) {
	file, err := blobs.store.CopyTo(ctx, value, directory, name)
	if err != nil {
		return filestore.Metadata{}, fmt.Errorf("prepare review cover: %w", err)
	}
	return file, nil
}
