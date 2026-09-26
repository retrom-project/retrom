package composition

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryReviewQueue(database dbapi.DB, tags *tagging.Service) *application.ReviewQueue {
	return application.NewReviewQueue(repository.NewReviewQueue(database), tags)
}

func NewLibraryReviewDetails(database dbapi.DB) *application.ReviewDetails {
	return application.NewReviewDetails(repository.NewReviewDetail(database))
}
