package libraryimport

import (
	"context"

	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

// TransitionReviewOwners updates the source and its aggregate in the caller's transaction.
func TransitionReviewOwners(
	ctx context.Context, executor dbexec.Executor, change application.ReviewOwnerTransition,
) error {
	if change.State != application.ReviewOwnerPublished && change.State != application.ReviewOwnerDiscarded {
		return application.ErrInvalid
	}
	pegasusAffected, err := transitionServerReviewOwner(ctx, executor, "source_import_items", change)
	if err != nil {
		return err
	}
	if pegasusAffected == 0 {
		return nil
	}
	return refreshSourceReviewCounts(ctx, executor, change.ItemID, change.NowMS)
}
