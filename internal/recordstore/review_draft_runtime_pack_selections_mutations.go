package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewDraftRuntimePackSelections(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_draft_runtime_pack_selections",
		"review_draft_id,slot",
		ReviewDraftRuntimePackSelectionsUpdateRule,
	)
}

const ReviewDraftRuntimePackSelectionsUpdateRule = `
WITH previous(review_draft_id,slot) AS (VALUES(?,?))
SELECT CASE
-- review_draft_runtime_pack_selections_validate_update
WHEN (1=1) THEN 'replace runtime pack selection atomically'
ELSE '' END
FROM review_draft_runtime_pack_selections candidate CROSS JOIN previous
WHERE candidate.review_draft_id=previous.review_draft_id AND candidate.slot=previous.slot`

func DeleteReviewDraftRuntimePackSelections(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"review_draft_runtime_pack_selections",
		"review_draft_id,slot",
		ReviewDraftRuntimePackSelectionsDeleteRule,
	)
}

const ReviewDraftRuntimePackSelectionsDeleteRule = `
WITH previous(review_draft_id,slot) AS (VALUES(?,?))
SELECT CASE
-- review_draft_runtime_pack_selections_validate_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
  WHERE draft.id=previous.review_draft_id AND item.state='REVIEW_PENDING'
)) THEN 'finalized review runtime pack selection'
ELSE '' END
FROM previous`
