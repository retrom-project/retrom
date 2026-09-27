package payloadrelease

import (
	"context"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/recordstore"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	application "retrom/internal/service/payloadrelease"
)

func (records scheduling) BeginRelease(ctx context.Context, change application.OwnerRelease) error {
	before := change.Before
	update := recordstore.Update{
		Set:    "payload_state='RELEASING',payload_release_job_id=?,version=version+1",
		Values: []any{change.JobID},
		Scope: recordstore.Scope{
			Where: "id=? AND version=? AND payload_state='RETAINED' AND COALESCE(payload_release_job_id,'')=?",
			Args:  []any{before.Scope.ID, before.Version, before.ReleaseJobID},
		},
	}
	switch before.Scope.Type {
	case application.ScopeGame:
		return gamerelease.Begin(ctx, records.executor, update, change)
	case application.ScopeImportItem, application.ScopeImportJob:
		return itemrelease.Begin(ctx, records.executor, update, change)
	case application.ScopeSourceImportItem:
		return sourcerelease.Begin(ctx, records.executor, update, change)
	default:
		return application.ErrScopeInvalid
	}

}
