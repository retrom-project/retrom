package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewUploadedAssets(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_uploaded_assets",
		"id",
		ReviewUploadedAssetsUpdateRule,
	)
}

const ReviewUploadedAssetsUpdateRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- review_uploaded_assets_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM review_uploaded_assets candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteReviewUploadedAssets(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"review_uploaded_assets",
		"id,import_item_id",
		ReviewUploadedAssetsDeleteRule,
	)
}

const ReviewUploadedAssetsDeleteRule = `
WITH previous(id,import_item_id) AS (VALUES(?,?))
SELECT CASE
-- review_uploaded_assets_immutable_delete
WHEN (NOT EXISTS(SELECT 1 FROM import_items WHERE id=previous.import_item_id AND payload_state IN
('RELEASING','FAILED'))) THEN 'immutable'
ELSE '' END
FROM previous`
