package gamerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	"retrom/internal/persistence/sessionstore"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) UnlinkSaves(ctx context.Context, gameID string) error {
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "launch_sessions",
		sessionstore.ChangeLaunch, recordstore.Update{
			Set: `save_state_id=NULL`,
			Scope: recordstore.Scope{
				Where: `game_id=? AND save_state_id IS NOT NULL`,
				Args:  []any{gameID},
			},
		}); err != nil {
		return fmt.Errorf("cleanupjobs/unlink launch saves: %w", err)
	}
	return nil
}
