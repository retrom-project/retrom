package gamerelease

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/fileownership"
	"retrom/internal/persistence/recordstore"
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
			fileownership.Owner{Kind: "GAME", ID: before.Scope.ID},
			change.NowMS,
		); err != nil {
			return nil, fmt.Errorf("change: %w", err)
		}
	}
	update.Set += ",updated_at_ms=?"
	update.Values = append(update.Values, change.NowMS)
	update.Scope.Where += ` AND status=? AND metadata_source_kind=? AND COALESCE(metadata_source_ref_id,'')=?
 AND content_source_kind=? AND COALESCE(content_source_ref_id,'')=?`
	update.Scope.Args = append(update.Scope.Args, before.State, change.Before.MetadataSource.Kind,
		change.Before.MetadataSource.ID, change.Before.ContentSource.Kind, change.Before.ContentSource.ID)
	return wrapPair(recordstore.UpdateGames(ctx, executor, update))
}
