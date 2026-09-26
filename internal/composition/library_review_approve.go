package composition

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

func NewLibraryReviewApprovals(database dbapi.DB, now func() time.Time) *application.ReviewApprovals {
	return application.NewReviewApprovals(repository.NewReviewApprovals(database),
		tagging.New(tagpersistence.New(database), now), now)
}
