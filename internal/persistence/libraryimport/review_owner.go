package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

// TransitionReviewOwners updates the source and its aggregate in the caller's transaction.
func TransitionReviewOwners(
	ctx context.Context, executor dbapi.Executor, change libraryservice.ReviewOwnerTransition,
) error {
	if change.State != libraryservice.ReviewOwnerPublished && change.State != libraryservice.ReviewOwnerDiscarded &&
		change.State != libraryservice.ReviewOwnerExisting {
		return libraryservice.ErrInvalid
	}
	var sourceImportID string
	err := dbapi.QueryRowContext(ctx, executor,
		`SELECT import_id FROM source_import_items WHERE library_import_item_id=?`, change.ItemID).Scan(&sourceImportID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read source review owner: %w", err)
	}
	sourceAffected, err := transitionServerReviewOwner(ctx, executor, "source_import_items", change)
	if err != nil {
		return err
	}
	if sourceAffected == 0 {
		return nil
	}
	return refreshSourceReviewCounts(ctx, executor, sourceImportID, change.NowMS)
}
