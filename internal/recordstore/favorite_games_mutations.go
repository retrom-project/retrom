package recordstore

import (
	"context"
	"database/sql"
)

func UpdateFavoriteGames(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"favorite_games",
		"profile_id,game_id",
		FavoriteGamesUpdateRule,
	)
}

const FavoriteGamesUpdateRule = `
WITH previous(profile_id,game_id) AS (VALUES(?,?))
SELECT CASE
-- favorite_games_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM favorite_games candidate CROSS JOIN previous
WHERE candidate.profile_id=previous.profile_id AND candidate.game_id=previous.game_id`
