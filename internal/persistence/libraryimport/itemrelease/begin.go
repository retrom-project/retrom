package itemrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/cleanupjobs"
)

func Begin(ctx context.Context, executor dbapi.Executor, update recordstore.Update,
	change application.OwnerRelease,
) error {
	update.Scope.Where += " AND state=?"
	update.Scope.Args = append(update.Scope.Args, change.Before.State)
	if change.Before.Scope.Type == application.ScopeImportItem {
		if err := releaseops.ScheduleWrite(recordstore.UpdateImportItems(ctx, executor, update)); err != nil {
			return fmt.Errorf("enter item payload release: %w", err)
		}
		return nil
	}
	args := append(append([]any{}, update.Values...), update.Scope.Args...)
	result, err := executor.ExecContext(ctx, "UPDATE import_jobs SET "+update.Set+" WHERE "+update.Scope.Where, args...)
	if err := releaseops.ScheduleWrite(result, err); err != nil {
		return fmt.Errorf("enter import job payload release: %w", err)
	}
	return nil
}
