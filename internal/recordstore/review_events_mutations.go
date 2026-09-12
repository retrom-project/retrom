package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewEvents(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_events",
		"id",
		ReviewEventsUpdateRule,
	)
}

const ReviewEventsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- review_events_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM review_events candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteReviewEvents(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"review_events",
		"id",
		ReviewEventsDeleteRule,
	)
}

const ReviewEventsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- review_events_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
