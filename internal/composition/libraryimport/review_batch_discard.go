package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReviewBatchDiscards(database dbapi.DB,
	discards *application.ReviewDiscards, now func() time.Time,
) *application.ReviewBatchDiscards {
	return application.NewReviewBatchDiscards(repository.NewReviewBatchDiscards(database), discards, now)
}
