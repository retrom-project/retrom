package recordstore

import (
	"context"
	"database/sql"
)

func UpdateAuditEvents(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"audit_events",
		"id",
		AuditEventsUpdateRule,
	)
}

const AuditEventsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- audit_events_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM audit_events candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteAuditEvents(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"audit_events",
		"id",
		AuditEventsDeleteRule,
	)
}

const AuditEventsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- audit_events_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
