package libraryimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"
)

const reviewBatchDiscardPageSize = 50

type ReviewBatchDiscards struct {
	repository model.ReviewBatchDiscardRepository
	discards   *ReviewDiscards
	now        func() time.Time
}

func NewReviewBatchDiscards(
	repository model.ReviewBatchDiscardRepository, discards *ReviewDiscards, now func() time.Time,
) *ReviewBatchDiscards {
	if now == nil {
		now = time.Now
	}
	return &ReviewBatchDiscards{repository: repository, discards: discards, now: now}
}

// DiscardBatch processes a bounded page so a large import does not monopolize
// a worker. Each item keeps the ordinary discard service's transaction and
// evidence rules.
func (service *ReviewBatchDiscards) DiscardBatch(ctx context.Context, importID string) (bool, error) {
	items, err := service.repository.Pending(ctx, importID, reviewBatchDiscardPageSize)
	if err != nil {
		return false, fmt.Errorf("list batch reviews: %w", err)
	}
	for _, item := range items {
		if _, err := service.discards.Discard(ctx, model.ReviewDiscardRequest{
			ItemID: item.ItemID, ExpectedVersion: item.Version,
			Reason: "丢弃本批次未发布内容", Mode: model.ReviewDiscardBatch,
		}); err != nil {
			return false, fmt.Errorf("discard batch review: %w", err)
		}
	}
	return len(items) < reviewBatchDiscardPageSize, nil
}

func (service *ReviewBatchDiscards) Release(ctx context.Context, importID string) error {
	if importID == "" {
		return model.ErrInvalid
	}
	if err := service.repository.Release(ctx, importID, service.now().UnixMilli()); err != nil {
		return fmt.Errorf("release discarded batch: %w", err)
	}
	return nil
}
