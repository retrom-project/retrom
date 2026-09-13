package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/persistence/dbexec"
)

func UpdateNetplayEvents(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"netplay_events",
		"id",
		NetplayEventsUpdateRule,
	)
}

const NetplayEventsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- netplay_events_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM netplay_events candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteNetplayEvents(
	ctx context.Context, db dbexec.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"netplay_events",
		"id",
		NetplayEventsDeleteRule,
	)
}

const NetplayEventsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- netplay_events_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
