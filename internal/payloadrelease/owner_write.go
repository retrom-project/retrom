package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/recordstore"
)

func updatePayloadOwner(
	ctx context.Context,
	tx *sql.Tx,
	scope ScopeType,
	change recordstore.Update,
) (sql.Result, error) {
	var result sql.Result
	var err error

	switch scope {
	case ScopeImportItem:
		result, err = recordstore.UpdateImportItems(ctx, tx, change)
	case ScopeGame:
		result, err = recordstore.UpdateGames(ctx, tx, change)
	case ScopePegasusImportItem:
		result, err = recordstore.UpdatePegasusImportItems(ctx, tx, change)
	case ScopeEmulationStationImportItem:
		result, err = recordstore.UpdateEmulationstationImportItems(ctx, tx, change)
	case ScopeImportJob:
		args := append(append([]any{}, change.Values...), change.Scope.Args...)
		result, err = tx.ExecContext(ctx, "UPDATE import_jobs SET "+change.Set+" WHERE "+change.Scope.Where, args...)
	case ScopeUploadConsumption, ScopeBlob:
		return nil, ErrScopeInvalid
	default:
		return nil, ErrScopeInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("payloadrelease owner: %w", err)
	}
	return result, nil
}
