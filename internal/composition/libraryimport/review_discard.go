package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewDiscards(database dbapi.DB, now func() time.Time) *application.ReviewDiscards {
	return application.NewReviewDiscards(repository.NewReviewDiscards(database), now)
}
