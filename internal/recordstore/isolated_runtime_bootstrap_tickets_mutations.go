package recordstore

import (
	"context"
	"database/sql"
)

func UpdateIsolatedRuntimeBootstrapTickets(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"isolated_runtime_bootstrap_tickets",
		"ticket_sha256,consumed_at_ms,expected_origin,expires_at_ms,launch_id,preview_id,"+
			"profile_id",
		IsolatedRuntimeBootstrapTicketsUpdateRule,
	)
}

const IsolatedRuntimeBootstrapTicketsUpdateRule = `
WITH previous(ticket_sha256,consumed_at_ms,expected_origin,expires_at_ms,launch_id,preview_id,
profile_id) AS (VALUES(?,?,?,?,?,?,?))
SELECT CASE
-- isolated_runtime_bootstrap_tickets_consume
WHEN (previous.consumed_at_ms IS NOT NULL
  OR candidate.ticket_sha256<>previous.ticket_sha256 OR candidate.launch_id IS NOT previous.launch_id
  OR candidate.preview_id IS NOT previous.preview_id
  OR candidate.profile_id<>previous.profile_id OR candidate.expected_origin<>previous.expected_origin
  OR candidate.expires_at_ms<>previous.expires_at_ms OR candidate.consumed_at_ms IS NULL
  OR NOT (
    previous.launch_id IS NOT NULL AND EXISTS(SELECT 1 FROM launch_sessions launch
      WHERE launch.id=previous.launch_id AND launch.state IN ('CREATED','ACTIVE')
        AND candidate.consumed_at_ms<=previous.expires_at_ms)
    OR previous.preview_id IS NOT NULL AND EXISTS(SELECT 1 FROM review_preview_sessions preview
      WHERE preview.id=previous.preview_id AND preview.state IN ('CREATED','ACTIVE')
        AND candidate.consumed_at_ms<=previous.expires_at_ms)
  )) THEN 'invalid bootstrap ticket consumption'
ELSE '' END
FROM isolated_runtime_bootstrap_tickets candidate CROSS JOIN previous
WHERE candidate.ticket_sha256=previous.ticket_sha256`

func DeleteIsolatedRuntimeBootstrapTickets(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"isolated_runtime_bootstrap_tickets",
		"ticket_sha256,launch_id,preview_id",
		IsolatedRuntimeBootstrapTicketsDeleteRule,
	)
}

const IsolatedRuntimeBootstrapTicketsDeleteRule = `
WITH previous(ticket_sha256,launch_id,preview_id) AS (VALUES(?,?,?))
SELECT CASE
-- isolated_runtime_bootstrap_tickets_immutable_delete
WHEN (previous.launch_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM review_preview_sessions preview
  WHERE preview.id=previous.preview_id AND preview.state NOT IN ('EXPIRED','REVOKED')
)) THEN 'bootstrap ticket is retained for audit'
ELSE '' END
FROM previous`
