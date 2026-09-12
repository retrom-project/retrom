package composition

import (
	"database/sql"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryReviewQueue(database *sql.DB, tags *tagging.Service) *application.ReviewQueue {
	return application.NewReviewQueue(repository.NewReviewQueue(database), tags)
}
