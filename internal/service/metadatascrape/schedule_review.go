package metadatascrape

import (
	"context"
	"fmt"
)

func (scheduler *Scheduler) ScheduleReview(
	ctx context.Context,
	itemID string,
	version int64,
	provider string,
) (Scheduled, int64, error) {
	var scheduled Scheduled
	err := scheduler.repository.WithWrite(ctx, func(scope ScheduleScope) error {
		draft, found, err := scope.Subjects.Review(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read review scrape subject: %w", err)
		}
		if !found || draft.Version != version {
			return ErrReviewVersionConflict
		}
		nonce, err := scheduleID()
		if err != nil {
			return err
		}
		now := scheduler.now().UnixMilli()
		scheduled, err = scheduler.scheduleImport(
			ctx,
			scope,
			itemID,
			provider,
			"metadata-review-v1:"+itemID+":"+nonce,
			true,
			now,
		)
		if err != nil {
			return err
		}
		return scope.Writes.Review(ctx, ReviewChange{ItemID: itemID, Version: draft.Version, Now: now})
	})
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("schedule review scrape: %w", err)
	}
	scheduler.start(ctx, scheduled)
	return scheduled, version + 1, nil
}

func (scheduler *Scheduler) ScheduleGame(ctx context.Context, id string, version int64) (Scheduled, int64, error) {
	var scheduled Scheduled
	err := scheduler.repository.WithWrite(ctx, func(scope ScheduleScope) error {
		game, found, err := scope.Subjects.Game(ctx, id)
		if err != nil {
			return fmt.Errorf("read game scrape subject: %w", err)
		}
		if !found || game.Version != version {
			return ErrGameVersionConflict
		}
		now := scheduler.now().UnixMilli()
		plan, err := newSchedulePlan(
			Subject{
				Kind: "GAME",
				ID:   id,
			},
			"HASHEOUS",
			"metadata-game-v1:"+id+":"+game.ManifestDigest,

			map[string]any{
				"gameId":               id,
				"sourceManifestDigest": game.ManifestDigest,
				"provider":             "HASHEOUS",
				"bypassCache":          true,
			},
			now,
		)
		if err != nil {
			return err
		}
		if err := scope.Writes.Create(ctx, plan); err != nil {
			return fmt.Errorf("create game scrape: %w", err)
		}
		if err := scheduleEvidence(ctx, scope, plan, game.PlatformID); err != nil {
			return err
		}
		if err := scope.Writes.Game(ctx, id, version, now); err != nil {
			return fmt.Errorf("advance game scrape version: %w", err)
		}
		scheduled = Scheduled{RunID: plan.RunID, JobID: plan.JobID}
		return nil
	})
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("schedule game scrape: %w", err)
	}
	scheduler.start(ctx, scheduled)
	return scheduled, version + 1, nil
}
