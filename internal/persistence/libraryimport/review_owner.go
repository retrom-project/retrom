package libraryimport

import (
	"context"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/libraryimport"
)

// TransitionReviewOwners updates the source and its aggregate in the caller's transaction.
func TransitionReviewOwners(
	ctx context.Context, executor dbexec.Executor, change application.ReviewOwnerTransition,
) error {
	if change.State != application.ReviewOwnerPublished && change.State != application.ReviewOwnerDiscarded {
		return application.ErrInvalid
	}
	pegasusAffected, err := transitionServerReviewOwner(ctx, executor, "pegasus_import_items", change)
	if err != nil {
		return err
	}
	emulationStationAffected, err := transitionServerReviewOwner(ctx, executor, "emulationstation_import_items", change)
	if err != nil {
		return err
	}
	if pegasusAffected+emulationStationAffected == 0 {
		return nil
	}
	if pegasusAffected > 0 {
		return refreshPegasusReviewCounts(ctx, executor, change.ItemID, change.NowMS)
	}
	return refreshEmulationStationReviewCounts(ctx, executor, change.ItemID, change.NowMS)
}
