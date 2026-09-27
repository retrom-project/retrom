package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewDeduplicator(database dbapi.DB, now func() time.Time) *application.ReviewDeduplicator {
	return application.NewReviewDeduplicator(repository.NewReviewDeduplicates(database), now)
}
