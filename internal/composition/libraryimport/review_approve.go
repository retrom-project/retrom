package libraryimport

import (
	"time"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewReviewApprovals(database dbapi.DB, now func() time.Time,
	files *filestore.Store, tags *tagging.Service,
) *application.ReviewApprovals {
	return application.NewReviewApprovals(repository.NewReviewApprovals(database),
		tags, now, files)
}
