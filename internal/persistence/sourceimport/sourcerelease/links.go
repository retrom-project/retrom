package sourcerelease

import (
	"context"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

func RetainedSources(
	ctx context.Context, executor dbapi.Executor, batch application.SourceBatch, after string, limit int,
) ([]string, error) {
	if batch.Type != application.ScopeSourceImportItem {
		return nil, application.ErrScopeInvalid
	}
	return wrapPair(dbapi.QueryStrings(ctx, executor, `SELECT id FROM source_import_items
 WHERE import_id=? AND payload_state='RETAINED' AND id>? ORDER BY id LIMIT ?`,
		batch.ImportID, after, limit))
}
