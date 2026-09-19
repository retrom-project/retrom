package metadatascrape

import (
	"errors"
	"io"

	"retrom/internal/model/blob"
)

var (
	ErrAssetStateConflict = errors.New("ASSET_STATE_CONFLICT")
	ErrGameDeleted        = errors.New("METADATA_GAME_DELETED")
)

type AssetPublication struct {
	ID            string
	Blob          blob.PreparedBlob
	MediaType     string
	Width, Height int
	Now           int64
}
type AssetBlobs interface {
	Put(io.Reader) (blob.PreparedBlob, error)
}
