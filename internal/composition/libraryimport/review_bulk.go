package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewBulkQueries(database dbapi.DB) *libraryservice.ReviewBulkQueries {
	return libraryservice.NewReviewBulkQueries(repository.NewReviewBulkQueries(database))
}

func NewReviewBulk(database dbapi.DB, approvals *libraryservice.ReviewApprovals,
	now func() time.Time,
) *libraryservice.ReviewBulk {
	return libraryservice.NewReviewBulk(repository.NewReviewBulk(database), approvals, now)
}
