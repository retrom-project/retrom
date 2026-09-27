package metadatascrape

import (
	"errors"
	"io"

	"retrom/internal/filestore"
)

var (
	ErrAssetStateConflict = errors.New("ASSET_STATE_CONFLICT")
	ErrGameDeleted        = errors.New("METADATA_GAME_DELETED")
)

type AssetPublication struct {
	ID            string
	Blob          filestore.Metadata
	MediaType     string
	Width, Height int
	Now           int64
}
type AssetBlobs interface {
	Put(io.Reader) (filestore.Metadata, error)
}
