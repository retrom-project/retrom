package recordstore

import (
	"context"
	"database/sql"
)

func UpdateFavoriteFolderGames(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"favorite_folder_games",
		"profile_id,folder_id,game_id",
		FavoriteFolderGamesUpdateRule,
	)
}

const FavoriteFolderGamesUpdateRule = `
WITH previous(profile_id,folder_id,game_id) AS (VALUES(?,?,?))
SELECT CASE
-- favorite_folder_games_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM favorite_folder_games candidate CROSS JOIN previous
WHERE candidate.profile_id=previous.profile_id AND candidate.folder_id=previous.folder_id AND
candidate.game_id=previous.game_id`
