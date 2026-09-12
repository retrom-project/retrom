package recordstore

import (
	"context"
	"database/sql"
)

func UpdateGameVariants(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"game_variants",
		"id,created_at_ms,game_id,updated_at_ms,version",
		GameVariantsUpdateRule,
	)
}

const GameVariantsUpdateRule = `
WITH previous(id,created_at_ms,game_id,updated_at_ms,version) AS (VALUES(?,?,?,?,?))
SELECT CASE
-- game_variants_guarded_update
WHEN (candidate.id<>previous.id OR candidate.game_id<>previous.game_id OR
candidate.created_at_ms<>previous.created_at_ms
  OR candidate.version<>previous.version+1 OR candidate.updated_at_ms<previous.updated_at_ms
  OR NOT EXISTS(
    SELECT 1 FROM runtime_target_bindings binding
    WHERE binding.core_id=candidate.core_id AND binding.provider_id=candidate.provider_id
      AND binding.target_id=candidate.target_id AND binding.launch_policy<>'DISABLED'
  )) THEN 'invalid current game runtime settings update'
ELSE '' END
FROM game_variants candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
