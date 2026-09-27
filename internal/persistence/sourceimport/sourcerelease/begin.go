package sourcerelease

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
	before := change.Before
	update.Scope.Where += " AND execution_state=? AND retryable=? AND COALESCE(library_import_item_id,'')=?"
	update.Scope.Args = append(update.Scope.Args, before.State, before.Retryable, before.PublicID)
	if err := releaseops.ScheduleWrite(recordstore.UpdateSourceImportItems(ctx, executor, update)); err != nil {
		return fmt.Errorf("enter source payload release: %w", err)
	}
	return nil
}
