package sourcerelease

import (
	"context"

	dbapi "retrom/internal/database"
)

func BlobIDs(ctx context.Context, executor dbapi.Executor, id string) ([]string, error) {
	return wrapPair(dbapi.QueryStrings(ctx, executor, `SELECT blob_id FROM source_import_item_files WHERE item_id=?
 UNION ALL SELECT source_archive_blob_id FROM source_import_item_files WHERE item_id=?
 UNION ALL SELECT blob_id FROM source_import_item_assets WHERE item_id=?`, id, id, id))
}
