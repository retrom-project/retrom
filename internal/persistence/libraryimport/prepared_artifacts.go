package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/blobstore"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobcatalog"
	application "retrom/internal/service/libraryimport"
)

type preparedArtifacts struct{ executor dbapi.Executor }

func BindPreparedArtifacts(executor dbapi.Executor) application.ImportArtifactWriter {
	return preparedArtifacts{executor: executor}
}

func (records preparedArtifacts) Register(
	ctx context.Context, metadata blobstore.Metadata, nowMS int64,
) (string, error) {
	id, err := blobcatalog.EnsureRecord(ctx, records.executor, metadata, "application/octet-stream", nowMS)
	if err != nil {
		return "", fmt.Errorf("register prepared import artifact: %w", err)
	}
	return id, nil
}
