package libraryimport

import (
	"fmt"
	"math"

	importprogressmodel "retrom/internal/model/importprogress"
	model "retrom/internal/model/libraryimport"
)

func projectReviewDiscardAggregate(before model.ReviewDiscardAggregate, now int64) (
	model.ReviewDiscardAggregateChange,
	error,
) {
	if before.Version < 1 || before.Version == math.MaxInt64 || before.Progress.Counts.ReviewPending < 1 {
		return model.ReviewDiscardAggregateChange{}, model.ErrInvalid
	}
	progress := before.Progress
	// A pending review has already completed the import pipeline.
	progress.Started = true
	progress.Counts.ReviewPending--
	projection, err := importprogressmodel.Project(progress, now)
	if err != nil {
		return model.ReviewDiscardAggregateChange{}, fmt.Errorf("project discarded import progress: %w", err)
	}
	return model.ReviewDiscardAggregateChange{
		ExpectedVersion: before.Version, ExpectedPending: before.Progress.Counts.ReviewPending,
		Projection: projection,
	}, nil
}
