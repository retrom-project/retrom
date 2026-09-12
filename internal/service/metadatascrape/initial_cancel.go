package metadatascrape

import (
	"context"
	"fmt"
)

func (service *InitialReviewService) Cancel(ctx context.Context, runID string, parentCancelled bool, now int64) error {
	item, active, err := service.active(ctx, runID)
	if err != nil || !active {
		return err
	}
	if !parentCancelled {
		return service.advanceReview(ctx, item, now)
	}
	change := initialProgress(item, now)
	change.ItemState = "CANCELLED"
	change.CancelledDelta = 1
	change.JobState = "CANCEL_REQUESTED"
	if item.Running == 1 {
		change.JobState = "CANCELLED"
	}
	if err := service.scope.Write.Advance(ctx, change); err != nil {
		return fmt.Errorf("cancel initial scrape progress: %w", err)
	}
	return nil
}
