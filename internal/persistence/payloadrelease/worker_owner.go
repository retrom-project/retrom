package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/recordstore"
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
	case application.ScopeImportItem:
		result, err = recordstore.UpdateImportItems(ctx, records.executor, update)
	case application.ScopeImportJob:
		args := append(append([]any{}, update.Values...), update.Scope.Args...)
		result, err = records.executor.ExecContext(ctx,
			"UPDATE import_jobs SET "+update.Set+" WHERE "+update.Scope.Where, args...)
	case application.ScopeSourceImportItem:
		result, err = recordstore.UpdateSourceImportItems(ctx, records.executor, update)

	case application.ScopeGame:
		update.Set += ",version=version+1,updated_at_ms=?"
		update.Values = append(update.Values, change.NowMS)
		result, err = recordstore.UpdateGames(ctx, records.executor, update)
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return application.ErrScopeInvalid
	default:
		return application.ErrScopeInvalid
	}
	if err := workerWrite(result, err); err != nil {
		return fmt.Errorf("save failed payload owner: %w", err)
	}
	return nil
}
