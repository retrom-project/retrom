package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewBulkQueries(database dbapi.DB) *application.ReviewBulkQueries {
	return application.NewReviewBulkQueries(repository.NewReviewBulkQueries(database))
}

func NewReviewBulk(database dbapi.DB, approvals *application.ReviewApprovals,
	now func() time.Time,
) *application.ReviewBulk {
	return application.NewReviewBulk(repository.NewReviewBulk(database), approvals, now)
}
