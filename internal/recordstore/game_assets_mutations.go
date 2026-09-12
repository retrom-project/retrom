package recordstore

import (
	"context"
	"database/sql"
)

func UpdateGameAssets(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"game_assets",
		"id,game_id",
		GameAssetsUpdateRule,
	)
}

const GameAssetsUpdateRule = `
WITH previous(id,game_id) AS (VALUES(?,?))
SELECT CASE
-- game_assets_owner_update
WHEN ((candidate.game_id IS NOT previous.game_id) AND (NOT EXISTS(SELECT 1 FROM games WHERE
id=candidate.game_id AND status='PUBLISHED'))) THEN 'game payload owner is not published'
ELSE '' END
FROM game_assets candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
