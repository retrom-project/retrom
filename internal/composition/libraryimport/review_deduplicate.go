package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewDeduplicator(database dbapi.DB, now func() time.Time) *libraryservice.ReviewDeduplicator {
	return libraryservice.NewReviewDeduplicator(repository.NewReviewDeduplicates(database), now)
}
