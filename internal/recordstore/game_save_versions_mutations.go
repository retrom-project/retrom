package recordstore

import (
	"context"
	"database/sql"
)

func UpdateGameSaveVersions(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"game_save_versions",
		"save_state_id,last_synced_at_ms",
		GameSaveVersionsUpdateRule,
	)
}

const GameSaveVersionsUpdateRule = `
WITH previous(save_state_id,last_synced_at_ms) AS (VALUES(?,?))
SELECT CASE
-- game_save_sync_time
WHEN ((candidate.last_synced_at_ms IS NOT previous.last_synced_at_ms) AND
(candidate.last_synced_at_ms<(SELECT created_at_ms FROM save_states WHERE id=candidate.save_state_id)))
THEN 'game save sync predates creation'
ELSE '' END
FROM game_save_versions candidate CROSS JOIN previous
WHERE candidate.save_state_id=previous.save_state_id`
