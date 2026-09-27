package sourcerelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
)

func ClearFiles(ctx context.Context, executor dbapi.Executor, id string, now int64) error {
	if _, err := recordstore.DeleteRows(ctx, executor, "source_import_item_companions", recordstore.Scope{
		Where: `rowid IN(SELECT rowid FROM source_import_item_companions WHERE item_id=? ORDER BY rowid LIMIT 200)`,
		Args:  []any{id},
	}); err != nil {
		return wrapErr(err)
	}
	return wrapErr((releaseops.Records{Executor: executor}).CheckedUpdate(ctx, "source_import_item_files",
		recordstore.UpdateSourceImportItemFiles, recordstore.Update{
			Set: `state='RELEASED',file_record=NULL,source_archive_file_record=NULL,source_archive_entry_ordinal=NULL,
    payload_released_at_ms=?,updated_at_ms=?`,
			Values: []any{now, now}, Scope: recordstore.Scope{
				Where: "item_id=? AND (file_record IS NOT NULL OR source_archive_file_record IS NOT NULL)", Args: []any{id},
			},
		}))
}

func ClearAssets(ctx context.Context, executor dbapi.Executor, id string, now int64) error {
	return wrapErr((releaseops.Records{Executor: executor}).CheckedUpdate(ctx, "source_import_item_assets",
		recordstore.UpdateSourceImportItemAssets, recordstore.Update{
			Set:    `state='RELEASED',file_record=NULL,payload_released_at_ms=?,updated_at_ms=?`,
			Values: []any{now, now}, Scope: recordstore.Scope{Where: "item_id=? AND file_record IS NOT NULL", Args: []any{id}},
		}))
}
