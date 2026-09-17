package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/pegasusimport"

	library "retrom/internal/model/libraryimport"
)

func prepareRecoveryReview(
	ctx context.Context,
	metadata model.ReviewMetadataSeeder,
	scope library.MetadataScope,
	execution model.ExecutionSnapshot,
	review model.ReviewHandoffSnapshot,
	now time.Time,
) (model.RecoveryReviewChange, error) {
	if review.Identity.JobID != execution.JobID || review.Identity.ImportID != execution.ImportID ||
		review.Identity.ExecutionNo != execution.ExecutionNo || review.Identity.Attempt != execution.Attempt ||
		review.Identity.LibraryItemID == "" || review.Identity.LibraryJobID == "" ||
		review.Version < 1 || review.Version == math.MaxInt64 {
		return model.RecoveryReviewChange{}, model.ErrVersionConflict
	}
	if review.State != "PENDING" && review.State != "COPYING" && review.State != "VALIDATING" {
		return model.RecoveryReviewChange{}, model.ErrVersionConflict
	}
	_, warnings, err := metadata.SeedInScope(
		ctx,
		scope,
		review.Identity.LibraryItemID,
		review.Metadata,
		now.UTC().Year()+1,
	)
	if err != nil {
		return model.RecoveryReviewChange{}, fmt.Errorf("seed recovered Pegasus review: %w", err)
	}
	return model.RecoveryReviewChange{Execution: execution, Handoff: model.ReviewHandoffChange{
		Before: review, Warnings: mergeReviewMetadataWarnings(review.Warnings, warnings), NowMS: now.UnixMilli(),
	}}, nil
}
