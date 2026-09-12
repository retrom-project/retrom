package recordstore

import (
	"context"
	"database/sql"
)

func CreateGameVariants(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateGameVariants)
}

func ValidateGameVariants(ctx context.Context, db DBTX, keys ...any) error {
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
