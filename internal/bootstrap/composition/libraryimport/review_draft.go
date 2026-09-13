package libraryimport

import (
	"database/sql"
	"time"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

// NewReviewDrafts wires the review draft application command to its SQL
// repository. Validation callbacks are kept as typed ports while the legacy
// validation slice is migrated independently.
func NewReviewDrafts(
	database *sql.DB,
	tags *tagging.Service,
	now func() time.Time,
	refresh repository.DraftValidationRefresher,
	selectScummVM repository.ScummVMSelector,
) *application.ReviewDrafts {
	return application.NewReviewDrafts(repository.NewReviewDraftPatches(database, repository.ReviewDraftPatchOptions{
		Tags: tags, Now: now, RefreshValidation: refresh, SelectScummVM: selectScummVM,
	}))
}
