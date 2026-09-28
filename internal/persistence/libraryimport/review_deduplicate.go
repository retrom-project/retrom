package libraryimport

import (
	"context"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

// ReviewDeduplicates owns the transaction-scoped readers and discard scope
// used by one bounded review deduplication pass.
type ReviewDeduplicates struct{ database dbapi.DB }

func NewReviewDeduplicates(database dbapi.DB) *ReviewDeduplicates {
	return &ReviewDeduplicates{database: database}
}

func (repository *ReviewDeduplicates) WithDeduplicate(
	ctx context.Context, work func(libraryservice.ReviewDeduplicateScope) error,
) error {
	return NewTransactions(repository.database).Write(ctx, func(executor dbapi.Executor) error {
		scope := libraryservice.ReviewDeduplicateScope{
			Reader:     BindReviewBulkQueries(executor),
			Duplicates: BindContentDuplicates(executor),
			Discard:    BindReviewDiscard(executor),
		}
		return work(scope)
	})
}

var _ libraryservice.ReviewDeduplicateRepository = (*ReviewDeduplicates)(nil)
