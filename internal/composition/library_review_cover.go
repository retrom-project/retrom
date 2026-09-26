package composition

import (
	"fmt"
	"io"
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/blobstore"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewLibraryReviewCoverUploads(
	database dbapi.DB, blobs *blobstore.Store, now func() time.Time,
) *application.ReviewCoverUploads {
	return application.NewReviewCoverUploads(repository.NewReviewCoverUploads(database), reviewCoverBlobs{blobs}, now)
}

type reviewCoverBlobs struct{ store *blobstore.Store }

func (blobs reviewCoverBlobs) OpenDigest(digest string) (io.ReadCloser, error) {
	file, err := blobs.store.OpenDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("open review cover blob: %w", err)
	}
	return file, nil
}
