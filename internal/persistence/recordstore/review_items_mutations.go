package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateReviewItems(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_items",
		"id,cover_candidate_asset_id,cover_uploaded_asset_id,effective_source_snapshot_id,"+
			"selected_validation_id",
		ReviewItemsUpdateRule,
	)
}

const ReviewItemsUpdateRule = `
WITH previous(id,cover_candidate_asset_id,cover_uploaded_asset_id,effective_source_snapshot_id,
selected_validation_id) AS (VALUES(?,?,?,?,?))
SELECT CASE
-- import_items_final_source_snapshot_update
WHEN ((candidate.effective_source_snapshot_id IS NOT previous.effective_source_snapshot_id) AND
(candidate.effective_source_snapshot_id<>previous.effective_source_snapshot_id
AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=previous.id AND
item.state<>'REVIEW_PENDING'))) THEN 'finalized review source snapshot'
-- import_items_source_snapshot_update
WHEN (candidate.effective_source_snapshot_id IS NOT previous.effective_source_snapshot_id AND
  candidate.review_version>0 AND (candidate.effective_source_snapshot_id IS NULL OR NOT
EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=candidate.effective_source_snapshot_id AND
snapshot.import_item_id=candidate.id
))) THEN 'invalid review source snapshot'
-- import_items_uploaded_cover_update
WHEN ((candidate.cover_candidate_asset_id IS
NOT previous.cover_candidate_asset_id OR candidate.cover_uploaded_asset_id IS NOT
previous.cover_uploaded_asset_id) AND (candidate.cover_uploaded_asset_id IS NOT NULL
AND (
  candidate.cover_candidate_asset_id IS NOT NULL
  OR NOT EXISTS (
    SELECT 1 FROM review_uploaded_assets a
    WHERE a.id=candidate.cover_uploaded_asset_id
    AND a.import_item_id=candidate.id
    AND a.kind='COVER'
  )
))) THEN 'invalid review uploaded cover'
-- import_items_validation_snapshot_update
WHEN ((candidate.effective_source_snapshot_id
IS NOT previous.effective_source_snapshot_id OR candidate.selected_validation_id IS NOT
previous.selected_validation_id) AND (candidate.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=candidate.selected_validation_id
  AND validation.import_item_id=candidate.id
  AND validation.source_snapshot_id=candidate.effective_source_snapshot_id
  AND validation.status='READY'
))) THEN 'invalid selected validation snapshot'
ELSE '' END
FROM import_items candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
