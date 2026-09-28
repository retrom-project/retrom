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
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bulk review: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(libraryservice.ReviewBulkStep{
		Worker: BindReviewBulkWorker(tx), Recovery: BindReviewBulkWorker(tx), Writes: BindReviewBulkWrites(tx),
		Candidates: BindReviewBulkQueries(tx), Approval: BindReviewApproval(tx),
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bulk review: %w", err)
	}
	return nil
}
