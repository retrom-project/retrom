package itemrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/filestore"
	"retrom/internal/persistence/filedeletion"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/cleanupjobs"
)

func Change(ctx context.Context, executor dbapi.Executor, update recordstore.Update,
	change application.EffectOwnerChange,
) (sql.Result, error) {
	before := change.Before.Owner
	if change.Released && before.Scope.Type == application.ScopeImportItem {
		if err := filedeletion.QueuePath(ctx, executor, filestore.ItemDirectory(before.Scope.ID), change.NowMS); err != nil {
			return nil, fmt.Errorf("change: %w", err)
		}
	}
	if before.Scope.Type == application.ScopeImportItem {
		update.Scope.Where += " AND state=? AND import_job_id=?"
		update.Scope.Args = append(update.Scope.Args, before.State, change.Before.ParentID)
		return wrapPair(recordstore.UpdateImportItems(ctx, executor, update))
	}
	update.Scope.Where += " AND state=?"
	update.Scope.Args = append(update.Scope.Args, before.State)
	args := append(append([]any{}, update.Values...), update.Scope.Args...)
	return wrapPair(
		executor.ExecContext(ctx, "UPDATE import_jobs SET "+update.Set+" WHERE "+update.Scope.Where, args...),
	)
}
