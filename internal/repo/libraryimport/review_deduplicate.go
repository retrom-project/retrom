package libraryimport

import (
	"database/sql"

	application "retrom/internal/model/libraryimport"
)

// ReviewDeduplicates owns the transaction-scoped readers and discard scope
// used by one bounded review deduplication pass.
type ReviewDeduplicates struct{ database *sql.DB }

func NewReviewDeduplicates(database *sql.DB) *ReviewDeduplicates {
	return &ReviewDeduplicates{database: database}
}

var _ application.ReviewDeduplicateRepository = (*ReviewDeduplicates)(nil)
