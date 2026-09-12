package recordstore

import (
	"context"
	"database/sql"
)

func UpdateJobEvents(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"job_events",
		"id",
		JobEventsUpdateRule,
	)
}

const JobEventsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- job_events_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM job_events candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteJobEvents(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"job_events",
		"id",
		JobEventsDeleteRule,
	)
}

const JobEventsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- job_events_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
