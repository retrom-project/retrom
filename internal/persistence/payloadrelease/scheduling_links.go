package payloadrelease

import (
	"context"

	dbapi "retrom/internal/database"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	application "retrom/internal/service/payloadrelease"
)

type sourceReleaseReader struct{ executor dbapi.Executor }

func BindReleases(executor dbapi.Executor) application.ReleaseScope {
	return application.ReleaseScope{Scheduling: BindScheduling(executor), Links: sourceReleaseReader{executor}}
}

func (reader sourceReleaseReader) RetainedSources(ctx context.Context, batch application.SourceBatch,
	after string, limit int,
) ([]string, error) {
	return wrapPair(sourcerelease.RetainedSources(ctx, reader.executor, batch, after, limit))
}
