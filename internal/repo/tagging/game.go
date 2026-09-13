package tagging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/tagging"
)

func (records gameRecords) Version(ctx context.Context, gameID string) (int64, error) {
	var version int64
	err := records.database.QueryRowContext(ctx, `SELECT version FROM games WHERE id=?`, gameID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, tagging.ErrGameNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("tagging: read game version: %w", err)
	}
	return version, nil
}

func (records gameRecords) Touch(ctx context.Context, gameID string, expectedVersion, now int64) error {
	updated, err := recordstore.UpdateGames(ctx, records.database, recordstore.Update{
		Set:   `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=?`, Args: []any{gameID, expectedVersion}}, Values: []any{now},
	})
	if err != nil {
		return fmt.Errorf("tagging: advance game version: %w", err)
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("tagging: read changed games: %w", err)
	}
	if affected != 1 {
		return tagging.ErrVersionConflict
	}
	return nil
}
