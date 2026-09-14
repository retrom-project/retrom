package emulationstationimport

import (
	"context"

	application "retrom/internal/model/emulationstationimport"
)

func (records recoveryRecords) Reviews(
	ctx context.Context,
	id string,
	limit int,
) ([]application.ExecutionReview, error) {
	return executionRecords(records).Reviews(ctx, id, limit)
}

func (records recoveryRecords) Fence(ctx context.Context, before application.LeaseSnapshot, now int64) error {
	return records.fence(ctx, application.RecoveryChange{Before: before, NowMS: now})
}

func (records recoveryRecords) CompleteReview(ctx context.Context, change application.ExecutionReviewCompletion) error {
	if err := records.Fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	return executionRecords(records).completeReviewProjection(ctx, change)
}
