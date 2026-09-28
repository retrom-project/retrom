package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewDiscards(database dbapi.DB, now func() time.Time) *libraryservice.ReviewDiscards {
	return libraryservice.NewReviewDiscards(repository.NewReviewDiscards(database), now)
}
