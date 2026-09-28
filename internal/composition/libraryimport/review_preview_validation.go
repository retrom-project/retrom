package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewPreviewValidations(
	database dbapi.DB,
	now func() time.Time,
	refresh repository.DraftValidationRefresher,
) *libraryservice.ReviewPreviewValidations {
	return libraryservice.NewReviewPreviewValidations(
		repository.NewReviewPreviewValidationRepository(database, refresh), now,
	)
}
