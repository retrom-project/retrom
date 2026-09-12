package libraryimport

import (
	"fmt"
	"math"

	"retrom/internal/service/importprogress"
)

func projectReviewDiscardAggregate(before ReviewDiscardAggregate, now int64) (ReviewDiscardAggregateChange, error) {
	if before.Version < 1 || before.Version == math.MaxInt64 || before.Progress.Counts.ReviewPending < 1 {
		return ReviewDiscardAggregateChange{}, ErrInvalid
	}
	progress := before.Progress
	// A pending review has already completed the import pipeline.
	progress.Started = true
	progress.Counts.ReviewPending--
	projection, err := importprogress.Project(progress, now)
	if err != nil {
		return ReviewDiscardAggregateChange{}, fmt.Errorf("project discarded import progress: %w", err)
	}
	return ReviewDiscardAggregateChange{
		ExpectedVersion: before.Version, ExpectedPending: before.Progress.Counts.ReviewPending,
		Projection: projection,
	}, nil
}
