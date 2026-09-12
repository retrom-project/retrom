package recordstore

import (
	"context"
	"database/sql"
)

func UpdateGames(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"games",
		"id,created_at_ms,status,updated_at_ms,version",
		GamesUpdateRule,
	)
}

const GamesUpdateRule = `
WITH previous(id,created_at_ms,status,updated_at_ms,version) AS (VALUES(?,?,?,?,?))
SELECT CASE
-- games_deleted_is_terminal
WHEN ((candidate.status IS NOT previous.status) AND (previous.status='DELETED' AND
candidate.status<>'DELETED')) THEN 'deleted game is terminal'
-- games_guarded_update
WHEN (candidate.id<>previous.id
  OR candidate.created_at_ms<>previous.created_at_ms
  OR candidate.version<>previous.version+1
  OR candidate.updated_at_ms<previous.updated_at_ms) THEN 'invalid current game update'
ELSE '' END
FROM games candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
