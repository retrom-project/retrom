package metadatascrape

import (
	"context"
	"fmt"

	model "retrom/internal/model/metadatascrape"

	"retrom/internal/capability/security/authn"
)

func (scheduler *Scheduler) ScheduleReview(
	ctx context.Context,
	itemID string,
	version int64,
	provider string,
) (Scheduled, int64, error) {
	result, err := scheduler.repository.CommitReviewSchedule(ctx, model.ReviewScheduleCommand{
		ItemID:   itemID,
		Provider: provider,
		Version:  version,
		Actor:    authn.ActorFromContext(ctx, "release-setup"),
		Now:      scheduler.now().UnixMilli(),
	})
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("schedule review scrape: %w", err)
	}
	scheduled := Scheduled{RunID: result.RunID, JobID: result.JobID, Noop: result.Noop}
	scheduler.start(ctx, scheduled)
	return scheduled, version + 1, nil
}

func (scheduler *Scheduler) ScheduleGame(ctx context.Context, id string, version int64) (Scheduled, int64, error) {
	result, err := scheduler.repository.CommitGameSchedule(ctx, model.GameScheduleCommand{
		GameID:  id,
		Version: version,
		Now:     scheduler.now().UnixMilli(),
	})
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("schedule game scrape: %w", err)
	}
	scheduled := Scheduled{RunID: result.RunID, JobID: result.JobID}
	scheduler.start(ctx, scheduled)
	return scheduled, version + 1, nil
}
