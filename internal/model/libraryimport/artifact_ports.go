package libraryimport

import (
	"context"

	blobmodel "retrom/internal/model/blob"
)

type ImportArtifactWriter interface {
	Register(context.Context, blobmodel.PreparedBlob, int64) (string, error)
}
