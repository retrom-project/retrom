package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/recordstore"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	application "retrom/internal/service/payloadrelease"
)

func (records workerRecords) failOwner(ctx context.Context, change application.WorkChange) error {
	before := *change.OwnerFailure
	update := recordstore.Update{
		Set: "payload_state='FAILED',payload_last_error_code=?", Values: []any{change.ErrorCode},
		Scope: recordstore.Scope{
			Where: "id=? AND version=? AND payload_release_job_id=? AND payload_state=?",
			Args:  []any{before.Scope.ID, before.Version, before.ReleaseJobID, before.PayloadState},
		},
	}
	var result sql.Result
	var err error
	switch before.Scope.Type {
	case application.ScopeGame:
		result, err = gamerelease.Fail(ctx, records.executor, update, change)
	case application.ScopeImportItem, application.ScopeImportJob:
		result, err = itemrelease.Fail(ctx, records.executor, update, before.Scope.Type)
	case application.ScopeSourceImportItem:
		result, err = sourcerelease.Fail(ctx, records.executor, update)
	default:
		return application.ErrScopeInvalid
	}

	if err := workerWrite(result, err); err != nil {
		return fmt.Errorf("save failed payload owner: %w", err)
	}
	return nil
}
