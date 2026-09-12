package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewPreviewFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_preview_files",
		"preview_session_id,role,logical_name",
		ReviewPreviewFilesUpdateRule,
	)
}

const ReviewPreviewFilesUpdateRule = `
WITH previous(preview_session_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- review_preview_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM review_preview_files candidate CROSS JOIN previous
WHERE candidate.preview_session_id=previous.preview_session_id AND candidate.role=previous.role AND
candidate.logical_name=previous.logical_name`

func DeleteReviewPreviewFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"review_preview_files",
		"preview_session_id,role,logical_name",
		ReviewPreviewFilesDeleteRule,
	)
}

const ReviewPreviewFilesDeleteRule = `
WITH previous(preview_session_id,role,logical_name) AS (VALUES(?,?,?))
SELECT CASE
-- review_preview_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM review_preview_sessions preview JOIN import_items item ON item.id=preview.import_item_id
  WHERE preview.id=previous.preview_session_id AND item.payload_state IN ('RELEASING','FAILED')
)) THEN 'immutable'
ELSE '' END
FROM previous`
