package libraryimport

import (
	"context"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

// TransitionReviewOwners updates the source and its aggregate in the caller's transaction.
func TransitionReviewOwners(
	ctx context.Context, executor dbapi.Executor, change libraryservice.ReviewOwnerTransition,
) error {
	if change.State != libraryservice.ReviewOwnerPublished && change.State != libraryservice.ReviewOwnerDiscarded {
		return libraryservice.ErrInvalid
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
