package sourcerelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/cleanupjobs"
)

func Clear(ctx context.Context, executor dbapi.Executor, change application.EffectRemoval) error {
	now := change.NowMS
	update := recordstore.Update{
		Set:    `state='RELEASED',file_record=NULL,payload_released_at_ms=?,updated_at_ms=?`,
		Values: []any{now, now},
		Scope: recordstore.Scope{
			Where: "item_id=? AND file_record IS NOT NULL",
			Args:  []any{change.Before.Owner.Scope.ID},
		},
	}
	table, write := "source_import_item_assets", recordstore.UpdateSourceImportItemAssets
	if change.Group == application.EffectSourceFiles {
		if _, err := recordstore.DeleteRows(ctx, executor, "source_import_item_companions", recordstore.Scope{
			Where: `rowid IN(SELECT rowid FROM source_import_item_companions WHERE item_id=? ORDER BY rowid LIMIT 200)`,
			Args:  []any{change.Before.Owner.Scope.ID},
		}); err != nil {
			return wrapErr(err)
		}
		table, write = "source_import_item_files", recordstore.UpdateSourceImportItemFiles
		update.Set = `state='RELEASED',file_record=NULL,source_archive_file_record=NULL,source_archive_entry_ordinal=NULL,
  payload_released_at_ms=?,updated_at_ms=?`
		update.Scope.Where = "item_id=? AND (file_record IS NOT NULL OR source_archive_file_record IS NOT NULL)"
	}
	return wrapErr((releaseops.Records{Executor: executor}).CheckedUpdate(ctx, table, write, update))
}
