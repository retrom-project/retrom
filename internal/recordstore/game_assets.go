package recordstore

import (
	"context"
	"database/sql"
)

func CreateGameAssets(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateGameAssets)
}

func ValidateGameAssets(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, game_assetsOwnership, keys)
}

const game_assetsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM games WHERE id=candidate.game_id AND status='PUBLISHED')) THEN
'game payload owner is not published'
ELSE '' END
FROM game_assets candidate
WHERE candidate.id=?`
