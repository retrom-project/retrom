package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewDraftTags(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_draft_tags",
		"review_draft_id,tag_id",
		ReviewDraftTagsUpdateRule,
	)
}

const ReviewDraftTagsUpdateRule = `
WITH previous(review_draft_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- review_draft_tags_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM review_draft_tags candidate CROSS JOIN previous
WHERE candidate.review_draft_id=previous.review_draft_id AND candidate.tag_id=previous.tag_id`

func DeleteReviewDraftTags(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"review_draft_tags",
		"review_draft_id,tag_id",
		ReviewDraftTagsDeleteRule,
	)
}

const ReviewDraftTagsDeleteRule = `
WITH previous(review_draft_id,tag_id) AS (VALUES(?,?))
SELECT CASE
-- review_draft_tags_validate_delete
WHEN (EXISTS(SELECT 1 FROM tags WHERE id=previous.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM review_drafts draft
    JOIN import_items item ON item.id=draft.import_item_id
    WHERE draft.id=previous.review_draft_id AND item.state='REVIEW_PENDING'
  )) THEN 'review tag mapping is frozen'
ELSE '' END
FROM previous`
