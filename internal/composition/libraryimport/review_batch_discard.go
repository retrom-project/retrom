package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewBatchDiscards(database dbapi.DB,
	discards *libraryservice.ReviewDiscards, now func() time.Time,
) *libraryservice.ReviewBatchDiscards {
	return libraryservice.NewReviewBatchDiscards(repository.NewReviewBatchDiscards(database), discards, now)
}
