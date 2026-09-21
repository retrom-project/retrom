package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewPreviewValidations(
	database *sql.DB,
	now func() time.Time,
	refresh repository.DraftValidationRefresher,
) *application.ReviewPreviewValidations {
	return application.NewReviewPreviewValidations(
		repository.NewReviewPreviewValidationRepository(database, refresh), now,
	)
}
