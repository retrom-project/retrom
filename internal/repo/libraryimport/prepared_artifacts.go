package libraryimport

import (
	"context"
	"fmt"

	blobmodel "retrom/internal/model/blob"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/dbexec"
)

type preparedArtifacts struct{ executor dbexec.Executor }

func BindPreparedArtifacts(executor dbexec.Executor) application.ImportArtifactWriter {
	return preparedArtifacts{executor: executor}
}

func (records preparedArtifacts) Register(
	ctx context.Context, metadata blobmodel.PreparedBlob, nowMS int64,
) (string, error) {
	id, err := blobcatalog.EnsureRecord(ctx, records.executor, metadata, "application/octet-stream", nowMS)
	if err != nil {
		return "", fmt.Errorf("register prepared import artifact: %w", err)
	}
	return id, nil
}
