package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewPreviewValidations(
	database *sql.DB,
	now func() time.Time,
) *application.ReviewPreviewValidations {
	reviewValidation := repository.BindReviewValidation(database)
	validation := application.NewReviewDraftValidationResolver(reviewValidation, reviewValidation, now)
	return application.NewReviewPreviewValidations(
		repository.NewReviewPreviewValidationRepository(database), validation, now,
	)
}
