package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewPreviewValidations(
	database dbapi.DB,
	now func() time.Time,
	refresh repository.DraftValidationRefresher,
) *application.ReviewPreviewValidations {
	return application.NewReviewPreviewValidations(
		repository.NewReviewPreviewValidationRepository(database, refresh), now,
	)
}
