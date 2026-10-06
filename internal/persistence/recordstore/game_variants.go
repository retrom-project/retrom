package recordstore

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
)

// NextEmulatorGameID reserves a number independently of transaction rollback.
// All variant creation paths share the bounded PostgreSQL sequence.
func NextEmulatorGameID(ctx context.Context, db dbapi.Queryer) (int64, error) {
	var id int64
	if err := dbapi.QueryRowContext(ctx, db, `SELECT nextval('emulator_game_numbers')`).Scan(&id); err != nil {
		return 0, fmt.Errorf("allocate emulator game number: %w", err)
	}
	return id, nil
}

func CreateGameVariants(
	ctx context.Context, db dbapi.Executor, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "game_variants", "id", ValidateGameVariants)
}

func ValidateGameVariants(ctx context.Context, db dbapi.Executor, keys ...any) error {
	return validate(ctx, db, game_variantsOwnership, keys)
}

const game_variantsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM games WHERE id=candidate.game_id AND status='PUBLISHED')
  OR NOT EXISTS(
    SELECT 1 FROM runtime_target_bindings binding
    WHERE binding.core_id=candidate.core_id AND binding.provider_id=candidate.provider_id
      AND binding.target_id=candidate.target_id AND binding.launch_policy<>'DISABLED'
  )) THEN 'invalid current game runtime settings'
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM game_variants candidate
WHERE candidate.id=?`
