package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewPreviewFiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "preview_session_id,role,logical_name", ValidateReviewPreviewFiles)
}

func ValidateReviewPreviewFiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_preview_filesOwnership, keys)
}

const review_preview_filesOwnership = `
SELECT CASE
WHEN (candidate.role='PROJECT_FILE' AND NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN import_item_source_snapshot_files source
    ON source.source_snapshot_id=preview.source_snapshot_id
    AND source.role='PROJECT_FILE'
    AND source.logical_name=candidate.logical_name
    AND source.blob_id=candidate.blob_id
  WHERE preview.id=candidate.preview_session_id
)) THEN 'invalid review preview project file'
WHEN (candidate.role='RUNTIME_FILE' AND NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  WHERE preview.id=candidate.preview_session_id AND (
    EXISTS(SELECT 1 FROM import_item_validation_files file
      WHERE file.import_item_core_validation_id=preview.validation_id AND file.blob_id=candidate.blob_id)
    OR EXISTS(SELECT 1 FROM review_drafts draft
      JOIN review_draft_runtime_pack_selections selection ON selection.review_draft_id=draft.id
      JOIN runtime_asset_pack_installations installation ON installation.id=selection.installation_id
      WHERE draft.import_item_id=preview.import_item_id
        AND draft.effective_source_snapshot_id=preview.source_snapshot_id
        AND installation.status='READY' AND installation.bundle_blob_id=candidate.blob_id)
  )
)) THEN 'invalid review preview runtime file'
ELSE '' END
FROM review_preview_files candidate
WHERE candidate.preview_session_id=? AND candidate.role=? AND candidate.logical_name=?`
