package composition

import (
	"database/sql"
	"time"

	repository "retrom/internal/repo/libraryimport"
	tagpersistence "retrom/internal/repo/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryReviewApprovals(database *sql.DB, now func() time.Time) *application.ReviewApprovals {
	return application.NewReviewApprovals(repository.NewReviewApprovals(database),
		tagging.New(tagpersistence.New(database), now), now)
}
