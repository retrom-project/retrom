package libraryimport

import (
	"context"
	"errors"
	"fmt"

	libraryimportmodel "retrom/internal/model/libraryimport"
	librarypersistence "retrom/internal/repo/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

// DiscardBatchReviews records ordinary review decisions after execution has stopped.
// It processes a bounded group so large batches do not monopolize a worker.
func (service *Service) DiscardBatchReviews(ctx context.Context, importID string) (bool, error) {
	result, err := libraryservice.NewReviewBatchDiscards(
		librarypersistence.NewReviewBatchDiscards(service.database), service.reviewDiscards(), service.now,
	).DiscardBatch(ctx, importID)
	if err != nil {
		return false, fmt.Errorf("libraryimport/discard batch review: %w", err)
	}
	return result, nil
}

// ReleaseDiscardedBatch closes rejected-only imports too. Published item ownership
// stays protected by Game references, independently of this import envelope.
func (service *Service) ReleaseDiscardedBatch(ctx context.Context, importID string) error {
	if err := libraryservice.NewReviewBatchDiscards(
		librarypersistence.NewReviewBatchDiscards(service.database), service.reviewDiscards(), service.now,
	).Release(ctx, importID); err != nil {
		if errors.Is(err, libraryimportmodel.ErrInvalid) {
			return ErrInvalid
		}
		return fmt.Errorf("libraryimport/release discarded batch: %w", err)
	}
	return nil
}
