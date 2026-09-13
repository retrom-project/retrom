package libraryimport

import (
	"database/sql"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

// NewReviewBulkQueries composes the read-only review bulk application service
// with its SQL repository. Mutating bulk workflows remain on the legacy facade
// until their worker and transaction writes are migrated as one unit.
func NewReviewBulkQueries(database *sql.DB) *application.ReviewBulkQueries {
	return application.NewReviewBulkQueries(repository.NewReviewBulkQueries(database))
}
