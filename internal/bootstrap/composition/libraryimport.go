package composition

import (
	"database/sql"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryReviewQueue(database *sql.DB, tags *tagging.Service) *application.ReviewQueue {
	return application.NewReviewQueue(repository.NewReviewQueue(database), tags)
}

func NewLibraryReviewDetails(database *sql.DB) *application.ReviewDetails {
	return application.NewReviewDetails(repository.NewReviewDetail(database))
}
