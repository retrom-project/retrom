package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewMultidiscAttachments(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewMultidiscAttachments)
}

func ValidateReviewMultidiscAttachments(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_multidisc_attachmentsOwnership, keys)
}

const review_multidisc_attachmentsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=candidate.base_source_snapshot_id
  JOIN jobs job ON job.id=candidate.job_id
  WHERE draft.id=candidate.review_draft_id AND draft.import_item_id=candidate.import_item_id
  AND draft.effective_source_snapshot_id=candidate.base_source_snapshot_id
  AND snapshot.import_item_id=candidate.import_item_id AND snapshot.content_kind='MULTI_DISC'
  AND job.scope_type='IMPORT_ITEM' AND job.scope_id=candidate.import_item_id
  AND job.kind='REVIEW_MULTI_DISC_VALIDATE'
)) THEN 'invalid multi-disc attachment owner'
ELSE '' END
FROM review_multidisc_attachments candidate
WHERE candidate.id=?`
