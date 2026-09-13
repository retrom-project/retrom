package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewDeduplicator(database *sql.DB, now func() time.Time) *application.ReviewDeduplicator {
	return application.NewReviewDeduplicator(repository.NewReviewDeduplicates(database), now)
}
