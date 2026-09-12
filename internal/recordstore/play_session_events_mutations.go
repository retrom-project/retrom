package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePlaySessionEvents(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"play_session_events",
		"play_session_id,client_sequence",
		PlaySessionEventsUpdateRule,
	)
}

const PlaySessionEventsUpdateRule = `
WITH previous(play_session_id,client_sequence) AS (VALUES(?,?))
SELECT CASE
-- play_session_events_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM play_session_events candidate CROSS JOIN previous
WHERE candidate.play_session_id=previous.play_session_id AND
candidate.client_sequence=previous.client_sequence`

func DeletePlaySessionEvents(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"play_session_events",
		"play_session_id,client_sequence",
		PlaySessionEventsDeleteRule,
	)
}

const PlaySessionEventsDeleteRule = `
WITH previous(play_session_id,client_sequence) AS (VALUES(?,?))
SELECT CASE
-- play_session_events_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
