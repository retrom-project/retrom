package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

// NewReviewDrafts binds each validation callback to the active review transaction.
func NewReviewDrafts(
	database dbapi.DB,
	tags *tagging.Service,
	now func() time.Time,
) *libraryservice.ReviewDrafts {
	return libraryservice.NewReviewDrafts(repository.NewReviewDraftPatches(database, repository.ReviewDraftPatchOptions{
		Tags: tags, Now: now,
	}))
}
