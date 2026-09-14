package libraryimport

import (
	"context"

	"retrom/internal/adapter/files/blobstore"
)

type ImportArtifactWriter interface {
	Register(context.Context, blobstore.Metadata, int64) (string, error)
}
