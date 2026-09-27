package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/recordstore"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/payloadrelease"
)

func (records effectRecords) ChangeOwner(ctx context.Context, change application.EffectOwnerChange) error {
	if err := records.fenceOwner(ctx, change.Before); err != nil {
		return err
	}
	before, after := change.Before.Owner, change.After
	update := recordstore.Update{
		Set: `payload_state='RELEASING',payload_release_job_id=?,version=?,payload_last_error_code=NULL,
payload_released_at_ms=?`,
		Values: []any{after.ReleaseJobID, after.Version, effectReleaseTime(change)},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND payload_state=? AND COALESCE(payload_release_job_id,'')=?`,
			Args:  []any{before.Scope.ID, before.Version, before.PayloadState, before.ReleaseJobID},
		},
	}
	if change.Released {
		update.Set = `payload_state='RELEASED',payload_release_job_id=?,version=?,payload_last_error_code=NULL,
payload_released_at_ms=?`
	}
	var result sql.Result
	var err error
	switch before.Scope.Type {
	case application.ScopeGame:
		result, err = gamerelease.Change(ctx, records.executor, update, change)
	case application.ScopeImportItem, application.ScopeImportJob:
		result, err = itemrelease.Change(ctx, records.executor, update, change)
	case application.ScopeSourceImportItem:
		result, err = sourcerelease.Change(ctx, records.executor, update, change)
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return application.ErrScopeInvalid
	default:
		return application.ErrScopeInvalid
	}

	if err := effectCount(result, err, 1); err != nil {
		return fmt.Errorf("change released payload owner: %w", err)
	}
	return nil
}

func effectReleaseTime(change application.EffectOwnerChange) any {
	if change.Released {
		return change.NowMS
	}
	return nil
}

func (records effectRecords) Consume(ctx context.Context, change application.EffectConsumptionChange) error {
	return wrapErr(uploads.Consume(ctx, records.executor, change))
}
