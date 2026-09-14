package saves

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/saves"
	"retrom/internal/repo/recordstore"
)

func (repository *Repository) Rename(
	ctx context.Context,
	request application.RenameRequest,
) error {
	result, err := recordstore.UpdateSaveStates(ctx, repository.database, recordstore.Update{
		Set: `name=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND profile_id=? AND version=? AND deleted_at_ms IS NULL`,
			Args:  []any{request.SaveStateID, request.ProfileID, request.ExpectedVersion},
		},
		Values: []any{request.Name, request.UpdatedAtMS},
	})
	if err != nil {
		return fmt.Errorf("update save name: %w", err)
	}
	return repository.checkMutationResult(ctx, request.SaveStateID, request.ProfileID, result)
}

func (repository *Repository) Delete(
	ctx context.Context,
	request application.DeleteRequest,
) error {
	result, err := recordstore.UpdateSaveStates(ctx, repository.database, recordstore.Update{
		Set: `deleted_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND profile_id=? AND version=? AND deleted_at_ms IS NULL`,
			Args:  []any{request.SaveStateID, request.ProfileID, request.ExpectedVersion},
		},
		Values: []any{request.UpdatedAtMS, request.UpdatedAtMS},
	})
	if err != nil {
		return fmt.Errorf("delete save: %w", err)
	}
	return repository.checkMutationResult(ctx, request.SaveStateID, request.ProfileID, result)
}

func (repository *Repository) checkMutationResult(
	ctx context.Context,
	id, profileID string,
	result sql.Result,
) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count save mutation: %w", err)
	}
	if changed == 1 {
		return nil
	}
	var exists int
	err = repository.database.QueryRowContext(ctx,
		`SELECT 1 FROM save_states WHERE id=? AND profile_id=? AND deleted_at_ms IS NULL`,
		id, profileID,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check save mutation target: %w", err)
	}
	return application.ErrVersionConflict
}

var _ application.StateMutationRepository = (*Repository)(nil)
