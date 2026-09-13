package libraryimport

import (
	"context"
	"database/sql"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/libraryimport"
)

// ReviewDeduplicates owns the transaction-scoped readers and discard scope
// used by one bounded review deduplication pass.
type ReviewDeduplicates struct{ database *sql.DB }

func NewReviewDeduplicates(database *sql.DB) *ReviewDeduplicates {
	return &ReviewDeduplicates{database: database}
}

func (repository *ReviewDeduplicates) WithDeduplicate(
	ctx context.Context, work func(application.ReviewDeduplicateScope) error,
) error {
	return NewTransactions(repository.database).Write(ctx, func(executor dbexec.Executor) error {
		scope := application.ReviewDeduplicateScope{
			Reader:     BindReviewBulkQueries(executor),
			Duplicates: BindContentDuplicates(executor),
			Discard:    BindReviewDiscard(executor),
		}
		return work(scope)
	})
}

var _ application.ReviewDeduplicateRepository = (*ReviewDeduplicates)(nil)
