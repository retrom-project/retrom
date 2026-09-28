package libraryimport

import (
	"time"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewReviewApprovals(database dbapi.DB, now func() time.Time,
	files *filestore.Store, tags *tagging.Service,
) *libraryservice.ReviewApprovals {
	return libraryservice.NewReviewApprovals(repository.NewReviewApprovals(database),
		tags, now, files)
}
