package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

// NewReviewDrafts binds each validation callback to the active review transaction.
func NewReviewDrafts(
	database dbapi.DB,
	tags *tagging.Service,
	now func() time.Time,
	refresh repository.DraftValidationRefresher,
	selectScummVM repository.ScummVMSelector,
) *application.ReviewDrafts {
	return application.NewReviewDrafts(repository.NewReviewDraftPatches(database, repository.ReviewDraftPatchOptions{
		Tags: tags, Now: now, RefreshValidation: refresh, SelectScummVM: selectScummVM,
	}))
}
