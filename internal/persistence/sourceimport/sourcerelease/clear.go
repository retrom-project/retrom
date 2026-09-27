package sourcerelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/payloadrelease"
)

func Clear(ctx context.Context, executor dbapi.Executor, change application.EffectRemoval) error {
	now := change.NowMS
	update := recordstore.Update{
		Set:    `state='PAYLOAD_RELEASED',blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?`,
		Values: []any{now, now},
		Scope:  recordstore.Scope{Where: "item_id=? AND blob_id IS NOT NULL", Args: []any{change.Before.Owner.Scope.ID}},
	}
	table, write := "source_import_item_assets", recordstore.UpdateSourceImportItemAssets
	if change.Group == application.EffectSourceFiles {
		table, write = "source_import_item_files", recordstore.UpdateSourceImportItemFiles
		update.Set = `state='PAYLOAD_RELEASED',blob_id=NULL,source_archive_blob_id=NULL,source_archive_entry_ordinal=NULL,
  payload_released_at_ms=?,updated_at_ms=?`
		update.Scope.Where = "item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL)"
	}
	return wrapErr((releaseops.Records{Executor: executor}).CheckedUpdate(ctx, table, write, update))
}
