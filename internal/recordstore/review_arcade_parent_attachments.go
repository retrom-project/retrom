package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewArcadeParentAttachments(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewArcadeParentAttachments)
}

func ValidateReviewArcadeParentAttachments(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_arcade_parent_attachmentsOwnership, keys)
}

const review_arcade_parent_attachmentsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=candidate.base_source_snapshot_id
  WHERE draft.id=candidate.review_draft_id
  AND draft.import_item_id=candidate.import_item_id
  AND snapshot.import_item_id=candidate.import_item_id
)) THEN 'invalid attachment owner'
ELSE '' END
FROM review_arcade_parent_attachments candidate
WHERE candidate.id=?`
