package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewBulk struct {
	*ReviewBulkQueries
	database dbapi.DB
}

func NewReviewBulk(database dbapi.DB) *ReviewBulk {
	return &ReviewBulk{ReviewBulkQueries: NewReviewBulkQueries(database), database: database}
}

func (repository *ReviewBulk) WithStep(ctx context.Context, run func(libraryservice.ReviewBulkStep) error) error {
	// Publishing files is a separate step after the database decision commits.
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		return run(libraryservice.ReviewBulkStep{
			Worker: BindReviewBulkWorker(tx), Recovery: BindReviewBulkWorker(tx), Writes: BindReviewBulkWrites(tx),
			Candidates: BindReviewBulkQueries(tx), Approval: BindReviewApproval(tx),
		})
	})
	if err != nil {
		return fmt.Errorf("commit bulk review: %w", err)
	}
	return nil
}
