package sourcerelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseops"
)

func Remaining(ctx context.Context, executor dbapi.Executor, id string) (int64, error) {
	query := `SELECT (SELECT count(*) FROM source_import_item_files
 WHERE item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL))+
 (SELECT count(*) FROM source_import_item_assets WHERE item_id=? AND blob_id IS NOT NULL)+
(SELECT count(*) FROM source_import_item_companions WHERE item_id=?)`
	return wrapPair((releaseops.Records{Executor: executor}).ReadCount(ctx, query, id, id, id))
}
