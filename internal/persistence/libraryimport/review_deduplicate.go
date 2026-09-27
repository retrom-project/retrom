package libraryimport

import (
	"context"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/libraryimport"
)

// ReviewDeduplicates owns the transaction-scoped readers and discard scope
// used by one bounded review deduplication pass.
type ReviewDeduplicates struct{ database dbapi.DB }

func NewReviewDeduplicates(database dbapi.DB) *ReviewDeduplicates {
	return &ReviewDeduplicates{database: database}
}

func (repository *ReviewDeduplicates) WithDeduplicate(
	ctx context.Context, work func(application.ReviewDeduplicateScope) error,
) error {
	return NewTransactions(repository.database).Write(ctx, func(executor dbapi.Executor) error {
		scope := application.ReviewDeduplicateScope{
			Reader:     BindReviewBulkQueries(executor),
			Duplicates: BindContentDuplicates(executor),
			Discard:    BindReviewDiscard(executor),
		}
		return work(scope)
	})
}

var _ application.ReviewDeduplicateRepository = (*ReviewDeduplicates)(nil)
