package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	library "retrom/internal/service/libraryimport"
)

func prepareRecoveryReview(
	ctx context.Context,
	metadata ReviewMetadataSeeder,
	scope library.MetadataScope,
	execution ExecutionSnapshot,
	review ReviewHandoffSnapshot,
	now time.Time,
) (RecoveryReviewChange, error) {
	if review.Identity.JobID != execution.JobID || review.Identity.ImportID != execution.ImportID ||
		review.Identity.ExecutionNo != execution.ExecutionNo || review.Identity.Attempt != execution.Attempt ||
		review.Identity.LibraryItemID == "" || review.Identity.LibraryJobID == "" ||
		review.Version < 1 || review.Version == math.MaxInt64 {
		return RecoveryReviewChange{}, ErrVersionConflict
	}
	if review.State != "PENDING" && review.State != "COPYING" && review.State != "VALIDATING" {
		return RecoveryReviewChange{}, ErrVersionConflict
	}
	_, warnings, err := metadata.SeedInScope(
		ctx,
		scope,
		review.Identity.LibraryItemID,
		review.Metadata,
		now.UTC().Year()+1,
	)
	if err != nil {
		return RecoveryReviewChange{}, fmt.Errorf("seed recovered Pegasus review: %w", err)
	}
	return RecoveryReviewChange{Execution: execution, Handoff: ReviewHandoffChange{
		Before: review, Warnings: mergeReviewMetadataWarnings(review.Warnings, warnings), NowMS: now.UnixMilli(),
	}}, nil
}
