package gamerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/payloadrelease"
)

func Begin(ctx context.Context, executor dbapi.Executor, update recordstore.Update,
	change application.OwnerRelease,
) error {
	update.Scope.Where += " AND status=?"
	update.Scope.Args = append(update.Scope.Args, change.Before.State)
	if change.DeleteGame {
		update.Set = "status='DELETED'," + update.Set + ",deleted_at_ms=?,updated_at_ms=?"
		update.Values = append(update.Values, change.NowMS, change.NowMS)
	}
	if err := releaseops.ScheduleWrite(recordstore.UpdateGames(ctx, executor, update)); err != nil {
		return fmt.Errorf("enter game payload release: %w", err)
	}
	return nil
}
