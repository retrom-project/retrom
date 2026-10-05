package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateRuntimePreviewSessions(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"runtime_preview_sessions",
		"id,checkpoint_created_at_ms,checkpoint_format,checkpoint_payload_file_record,"+
			"restore_checkpoint_format,restore_from_preview_id,restore_payload_file_record",
		RuntimePreviewSessionsUpdateRule,
	)
}

const RuntimePreviewSessionsUpdateRule = `
WITH previous(id,checkpoint_created_at_ms,checkpoint_format,checkpoint_payload_file_record,
restore_checkpoint_format,restore_from_preview_id,restore_payload_file_record)
AS (VALUES(?::text,?::bigint,?::text,?::text,?::text,?::text,?::text))
SELECT CASE
-- review_preview_checkpoint_update
WHEN ((candidate.checkpoint_payload_file_record IS DISTINCT FROM previous.checkpoint_payload_file_record OR
candidate.checkpoint_format IS DISTINCT FROM previous.checkpoint_format OR candidate.checkpoint_created_at_ms
 IS DISTINCT FROM previous.checkpoint_created_at_ms) AND (candidate.checkpoint_payload_file_record IS NOT
 NULL AND
candidate.state<>'ACTIVE'
 OR candidate.checkpoint_payload_file_record IS NULL AND previous.checkpoint_payload_file_record IS NOT NULL
    AND candidate.state NOT IN ('EXPIRED','REVOKED'))) THEN 'invalid review checkpoint update'
-- review_preview_restore_immutable
WHEN ((candidate.restore_from_preview_id IS DISTINCT FROM previous.restore_from_preview_id OR
candidate.restore_payload_file_record IS DISTINCT FROM previous.restore_payload_file_record OR
candidate.restore_checkpoint_format IS DISTINCT FROM previous.restore_checkpoint_format) AND
(candidate.restore_from_preview_id IS DISTINCT FROM previous.restore_from_preview_id
 OR (candidate.restore_payload_file_record IS DISTINCT FROM previous.restore_payload_file_record
    OR candidate.restore_checkpoint_format IS DISTINCT FROM previous.restore_checkpoint_format)
    AND NOT (candidate.state IN ('EXPIRED','REVOKED') AND candidate.restore_payload_file_record IS NULL
      AND candidate.restore_checkpoint_format IS NULL))) THEN 'immutable review restore'
ELSE '' END
FROM runtime_preview_sessions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
