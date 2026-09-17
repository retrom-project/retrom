package libraryimport

import (
	"fmt"
	"math"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/model/importprogress"
)

func projectReviewDiscardAggregate(
	before model.ReviewDiscardAggregate,
	now int64,
) (model.ReviewDiscardAggregateChange, error) {
	if before.Version < 1 || before.Version == math.MaxInt64 || before.Progress.Counts.ReviewPending < 1 {
		return model.ReviewDiscardAggregateChange{}, model.ErrInvalid
	}
	progress := before.Progress
	// A pending review has already completed the import pipeline.
	progress.Started = true
	progress.Counts.ReviewPending--
	projection, err := importprogress.Project(progress, now)
	if err != nil {
		return model.ReviewDiscardAggregateChange{}, fmt.Errorf("project discarded import progress: %w", err)
	}
	return model.ReviewDiscardAggregateChange{
		ExpectedVersion: before.Version, ExpectedPending: before.Progress.Counts.ReviewPending,
		Projection: projection,
	}, nil
}
