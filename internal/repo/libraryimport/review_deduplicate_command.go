package libraryimport

import (
	"context"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

func (repository *ReviewDeduplicates) CommitDeduplicate(
	ctx context.Context, cmd application.DeduplicateCommand,
) (application.ReviewDeduplicateResult, error) {
	var result application.ReviewDeduplicateResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		reader := BindReviewBulkQueries(executor)
		duplicates := BindContentDuplicates(executor)

		through := cmd.Request.ThroughItemID
		if through == "" {
			value, err := reader.LatestReviewItemID(ctx)
			if err != nil {
				return fmt.Errorf("read deduplication upper bound: %w", err)
			}
			if value == nil {
				return nil
			}
			through = *value
		}

		candidates, err := reader.Candidates(ctx, application.ReviewBulkCandidateQuery{
			Scope:         cmd.Request.Scope,
			AfterItemID:   cmd.Request.AfterItemID,
			ThroughItemID: through,
			Limit:         reviewDeduplicatePageSize + 1,
		})
		if err != nil {
			return fmt.Errorf("read review duplicate candidates: %w", err)
		}
		result.ThroughItemID = &through
		if len(candidates) > reviewDeduplicatePageSize {
			candidates = candidates[:reviewDeduplicatePageSize]
			result.NextAfterItemID = &candidates[len(candidates)-1].ItemID
		}

		slotIdx := 0
		for _, candidate := range candidates {
			result.ScannedCount++
			if candidate.AttachmentActive {
				result.AttachmentActiveCount++
				continue
			}
			snapshot, err := duplicates.Snapshot(ctx, candidate.ItemID)
			if err != nil {
				return fmt.Errorf("read duplicate source snapshot: %w", err)
			}
			games, err := duplicates.PublishedMatches(ctx, application.DuplicateQuery{
				SnapshotID:  snapshot.ID,
				PlatformID:  candidate.PlatformID,
				ContentKind: snapshot.Kind,
			})
			if err != nil {
				return fmt.Errorf("read duplicate games: %w", err)
			}
			if len(games) == 0 {
				continue
			}
			if slotIdx >= len(cmd.Discards) {
				return fmt.Errorf("insufficient discard identity slots")
			}
			slot := cmd.Discards[slotIdx]
			slotIdx++
			discardCmd := application.DiscardCommand{
				Request: application.ReviewDiscardRequest{
					ItemID:          candidate.ItemID,
					ExpectedVersion: candidate.ReviewVersion,
					Reason:          "快速去重：游戏内容已发布",
					Mode:            application.ReviewDiscardSingle,
				},
				EventID: slot.EventID,
				Actor:   cmd.Actor,
				NowMS:   cmd.NowMS,
			}
			if _, err := applyDiscard(ctx, executor, discardCmd); err != nil {
				return fmt.Errorf("discard duplicate review: %w", err)
			}
			result.DiscardedCount++
		}
		return nil
	})
	if err != nil {
		return application.ReviewDeduplicateResult{}, fmt.Errorf("deduplicate review items: %w", err)
	}
	return result, nil
}

const reviewDeduplicatePageSize = 50
