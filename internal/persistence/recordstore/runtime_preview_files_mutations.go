package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateRuntimePreviewFiles(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"runtime_preview_files",
		"preview_session_id,role,logical_name",
		RuntimePreviewFilesUpdateRule,
	)
}

const RuntimePreviewFilesUpdateRule = `
WITH previous(preview_session_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- runtime_preview_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM runtime_preview_files candidate CROSS JOIN previous
WHERE candidate.preview_session_id=previous.preview_session_id AND candidate.role=previous.role AND
candidate.logical_name=previous.logical_name`

func DeleteRuntimePreviewFiles(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"runtime_preview_files",
		"preview_session_id,role,logical_name",
		RuntimePreviewFilesDeleteRule,
	)
}

const RuntimePreviewFilesDeleteRule = `
WITH previous(preview_session_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- runtime_preview_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_preview_sessions preview
  WHERE preview.id=previous.preview_session_id AND preview.state IN ('EXPIRED','REVOKED')
)) THEN 'immutable'
ELSE '' END
FROM previous`
