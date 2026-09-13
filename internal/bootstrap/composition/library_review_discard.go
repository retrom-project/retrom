package composition

import (
	"database/sql"
	"time"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewLibraryReviewDiscards(database *sql.DB, now func() time.Time) *application.ReviewDiscards {
	return application.NewReviewDiscards(repository.NewReviewDiscards(database), now)
}
