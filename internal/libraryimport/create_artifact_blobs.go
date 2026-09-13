package libraryimport

import (
	"fmt"
	"io"

	"retrom/internal/blobstore"
)

type creationArtifactBlobs struct{ store *blobstore.Store }

func (blobs creationArtifactBlobs) OpenDigest(digest string) (io.ReadCloser, error) {
	if blobs.store == nil {
		return nil, ErrInvalid
	}
	file, err := blobs.store.OpenDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("open import artifact source: %w", err)
	}
	return file, nil
}

func (blobs creationArtifactBlobs) Put(reader io.Reader) (blobstore.Metadata, error) {
	if blobs.store == nil {
		return blobstore.Metadata{}, ErrInvalid
	}
	metadata, err := blobs.store.Put(reader)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("store import artifact: %w", err)
	}
	return metadata, nil
}
