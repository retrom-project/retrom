package libraryimport

import (
	"context"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/libraryimport"
)

// TransitionReviewOwners updates the source and its aggregate in the caller's transaction.
func TransitionReviewOwners(
	ctx context.Context, executor dbapi.Executor, change application.ReviewOwnerTransition,
) error {
	if change.State != application.ReviewOwnerPublished && change.State != application.ReviewOwnerDiscarded {
		return application.ErrInvalid
	}
	sourceAffected, err := transitionServerReviewOwner(ctx, executor, "source_import_items", change)
	if err != nil {
		return err
	}
	if sourceAffected == 0 {
		return nil
	}
	return refreshSourceReviewCounts(ctx, executor, change.ItemID, change.NowMS)
}
