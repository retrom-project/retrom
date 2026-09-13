package metadatascrape

import (
	"errors"
	"io"

	"retrom/internal/blobstore"
)

var (
	ErrAssetStateConflict = errors.New("ASSET_STATE_CONFLICT")
	ErrGameDeleted        = errors.New("METADATA_GAME_DELETED")
)

type AssetPublication struct {
	ID            string
	Blob          blobstore.Metadata
	MediaType     string
	Width, Height int
	Now           int64
}
type AssetBlobs interface {
	Put(io.Reader) (blobstore.Metadata, error)
}
