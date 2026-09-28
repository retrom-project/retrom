package libraryimport

import (
	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewReviewQueue(database dbapi.DB, tags *tagging.Service) *libraryservice.ReviewQueue {
	return libraryservice.NewReviewQueue(repository.NewReviewQueue(database), tags)
}

func NewReviewDetails(database dbapi.DB) *libraryservice.ReviewDetails {
	return libraryservice.NewReviewDetails(repository.NewReviewDetail(database))
}
