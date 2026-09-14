package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/recordstore"
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
		update.Set += ",updated_at_ms=?"
		update.Values = append(update.Values, change.NowMS)
		update.Scope.Where += ` AND status=? AND metadata_source_kind=? AND COALESCE(metadata_source_ref_id,'')=?
AND content_source_kind=? AND COALESCE(content_source_ref_id,'')=?`
		update.Scope.Args = append(update.Scope.Args, before.State, change.Before.MetadataSource.Kind,
			change.Before.MetadataSource.ID, change.Before.ContentSource.Kind, change.Before.ContentSource.ID)
		result, err = recordstore.UpdateGames(ctx, records.executor, update)
	case application.ScopeImportItem:
		update.Scope.Where += " AND state=? AND import_job_id=?"
		update.Scope.Args = append(update.Scope.Args, before.State, change.Before.ParentID)
		result, err = recordstore.UpdateImportItems(ctx, records.executor, update)
	case application.ScopeImportJob:
		update.Scope.Where += " AND state=?"
		update.Scope.Args = append(update.Scope.Args, before.State)
		args := append(append([]any{}, update.Values...), update.Scope.Args...)
		result, err = records.executor.ExecContext(
			ctx,
			"UPDATE import_jobs SET "+update.Set+" WHERE "+update.Scope.Where,
			args...,
		)
	case application.ScopePegasusImportItem, application.ScopeEmulationStationImportItem:
		spec, specErr := effectSourceSpec(before.Scope.Type)
		if specErr != nil {
			return specErr
		}
		update.Scope.Where += ` AND execution_state=? AND retryable=?
AND COALESCE(library_import_item_id,'')=? AND import_id=? AND COALESCE(existing_game_id,'')=?`
		update.Scope.Args = append(
			update.Scope.Args,
			before.State,
			before.Retryable,
			before.PublicID,
			change.Before.ParentID,
			change.Before.ExistingGameID,
		)
		result, err = spec.updateItem(ctx, records.executor, update)
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
	before := change.Before
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE upload_consumptions SET released_at_ms=?,release_reason=?,version=version+1
WHERE id=? AND version=? AND released_at_ms IS NULL AND upload_session_id=? AND
COALESCE(upload_file_id,'')=? AND consumer_type=? AND consumer_id=?`,
		change.NowMS,
		change.Reason,
		before.ID,
		before.Version,
		before.SessionID,
		before.FileID,
		before.ConsumerType,
		before.ConsumerID,
	)
	if err := effectCount(result, err, 1); err != nil {
		return fmt.Errorf("write released consumption: %w", err)
	}
	return nil
}
