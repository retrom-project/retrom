package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/recordstore"
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
	var result sql.Result
	var err error
	switch before.Scope.Type {
	case application.ScopeImportItem:
		update.Scope.Where += " AND state=?"
		update.Scope.Args = append(update.Scope.Args, before.State)
		result, err = recordstore.UpdateImportItems(ctx, records.executor, update)
	case application.ScopeImportJob:
		update.Scope.Where += " AND state=?"
		update.Scope.Args = append(update.Scope.Args, before.State)
		args := append(append([]any{}, update.Values...), update.Scope.Args...)
		result, err = records.executor.ExecContext(ctx,
			"UPDATE import_jobs SET "+update.Set+" WHERE "+update.Scope.Where, args...)
	case application.ScopeGame:
		update.Scope.Where += " AND status=?"
		update.Scope.Args = append(update.Scope.Args, before.State)
		if change.DeleteGame {
			update.Set = "status='DELETED'," + update.Set + ",deleted_at_ms=?,updated_at_ms=?"
			update.Values = append(update.Values, change.NowMS, change.NowMS)
		}
		result, err = recordstore.UpdateGames(ctx, records.executor, update)
	case application.ScopeSourceImportItem:
		result, err = records.beginSourceRelease(ctx, update, before)
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return application.ErrScopeInvalid
	default:
		return application.ErrScopeInvalid
	}
	if err := schedulingWrite(result, err); err != nil {
		return fmt.Errorf("enter payload owner release: %w", err)
	}
	return nil
}

func (records scheduling) beginSourceRelease(
	ctx context.Context, update recordstore.Update, before application.Owner,
) (sql.Result, error) {
	update.Scope.Where += " AND execution_state=? AND retryable=? AND COALESCE(library_import_item_id,'')=?"
	update.Scope.Args = append(update.Scope.Args, before.State, before.Retryable, before.PublicID)
	result, err := recordstore.UpdateSourceImportItems(ctx, records.executor, update)
	if err != nil {
		return nil, fmt.Errorf("enter source payload release: %w", err)
	}
	return result, nil
}
