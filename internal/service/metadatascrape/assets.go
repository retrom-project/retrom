package metadatascrape

import (
	"context"
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
	Directory     string
	Blob          filestore.Metadata
	MediaType     string
	Width, Height int
	Now           int64
}
type AssetBlobs interface {
	Put(io.Reader) (filestore.Metadata, error)
	CopyTo(context.Context, string, string, string) (filestore.Metadata, error)
	RemovePath(context.Context, string) error
}
