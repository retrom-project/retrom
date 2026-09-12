package recordstore

import (
	"context"
	"database/sql"
)

func UpdateGameFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"game_files",
		"game_id,role,logical_name",
		GameFilesUpdateRule,
	)
}

const GameFilesUpdateRule = `
WITH previous(game_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- game_files_owner_update
WHEN ((candidate.game_id IS NOT previous.game_id) AND (NOT EXISTS(SELECT 1 FROM games WHERE
id=candidate.game_id AND status='PUBLISHED'))) THEN 'game payload owner is not published'
ELSE '' END
FROM game_files candidate CROSS JOIN previous
WHERE candidate.game_id=previous.game_id AND candidate.role=previous.role AND
candidate.logical_name=previous.logical_name`
