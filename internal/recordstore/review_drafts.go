package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewDrafts(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewDrafts)
}

func ValidateReviewDrafts(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_draftsOwnership, keys)
}

const review_draftsOwnership = `
SELECT CASE
WHEN (candidate.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=candidate.effective_source_snapshot_id AND
snapshot.import_item_id=candidate.import_item_id
)) THEN 'invalid review source snapshot'
WHEN (candidate.cover_uploaded_asset_id IS NOT NULL
AND (
  candidate.cover_candidate_asset_id IS NOT NULL
  OR NOT EXISTS (
    SELECT 1 FROM review_uploaded_assets a
    WHERE a.id=candidate.cover_uploaded_asset_id
    AND a.import_item_id=candidate.import_item_id
    AND a.kind='COVER'
  )
)) THEN 'invalid review uploaded cover'
WHEN (candidate.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=candidate.selected_validation_id
  AND validation.import_item_id=candidate.import_item_id
  AND validation.source_snapshot_id=candidate.effective_source_snapshot_id
  AND validation.status='READY'
)) THEN 'invalid selected validation snapshot'
ELSE '' END
FROM review_drafts candidate
WHERE candidate.id=?`
