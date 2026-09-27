package sourcerelease

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/fileownership"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/uploads/receivedfiles"
	application "retrom/internal/service/cleanupjobs"
)

func Change(ctx context.Context, executor dbapi.Executor, update recordstore.Update,
	change application.EffectOwnerChange,
) (sql.Result, error) {
	before := change.Before.Owner
	if change.Released {
		if err := fileownership.RetireAll(
			ctx,
			executor,
			fileownership.Owner{Kind: "SOURCE_IMPORT_ITEM", ID: before.Scope.ID},
			change.NowMS,
		); err != nil {
			return nil, fmt.Errorf("change: %w", err)
		}
		if err := receivedfiles.ReleaseRetired(
			ctx, executor, "SOURCE_IMPORT_ITEM", before.Scope.ID, change.NowMS,
		); err != nil {
			return nil, fmt.Errorf("release retired Source inputs: %w", err)
		}
	}
	update.Scope.Where += ` AND execution_state=? AND retryable=?
 AND COALESCE(library_import_item_id,'')=? AND import_id=? AND COALESCE(existing_game_id,'')=?`
	update.Scope.Args = append(update.Scope.Args, before.State, before.Retryable, before.PublicID,
		change.Before.ParentID, change.Before.ExistingGameID)
	return wrapPair(recordstore.UpdateSourceImportItems(ctx, executor, update))
}
