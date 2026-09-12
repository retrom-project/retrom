package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewDraftTags(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "review_draft_id,tag_id", ValidateReviewDraftTags)
}

func ValidateReviewDraftTags(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_draft_tagsOwnership, keys)
}

const review_draft_tagsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(SELECT 1 FROM tags WHERE id=candidate.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM review_drafts draft
    JOIN import_items item ON item.id=draft.import_item_id
    WHERE draft.id=candidate.review_draft_id AND item.state='REVIEW_PENDING'
  )
  OR (SELECT count(*) FROM review_draft_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.review_draft_id=candidate.review_draft_id)>20) THEN 'invalid active review tag'
ELSE '' END
FROM review_draft_tags candidate
WHERE candidate.review_draft_id=? AND candidate.tag_id=?`
