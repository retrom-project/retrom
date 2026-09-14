package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

// NewReviewDrafts wires the application planner to value-only repository and
// validation-fact ports. No repository callback can invoke the service layer.
func NewReviewDrafts(
	database *sql.DB,
	now func() time.Time,
) *application.ReviewDrafts {
	validation := application.NewReviewDraftValidationResolver(repository.BindReviewValidation(database), now)
	return application.NewReviewDrafts(repository.NewReviewDraftPatches(database), application.ReviewDraftsOptions{
		Validation: validation, Now: now,
	})
}
