package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewBulkApprovalItems(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "bulk_approval_id,import_item_id", ValidateReviewBulkApprovalItems)
}

func ValidateReviewBulkApprovalItems(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_bulk_approval_itemsOwnership, keys)
}

const review_bulk_approval_itemsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM import_items item
  JOIN review_drafts draft ON draft.import_item_id=item.id
  JOIN import_item_source_snapshots snapshot ON snapshot.id=candidate.expected_source_snapshot_id
  JOIN import_item_core_validations validation ON validation.id=candidate.expected_validation_id
  WHERE item.id=candidate.import_item_id AND item.state='REVIEW_PENDING'
  AND draft.version=candidate.expected_review_version
  AND draft.effective_source_snapshot_id=candidate.expected_source_snapshot_id
  AND draft.target_platform_instance_id=candidate.target_platform_instance_id
  AND snapshot.import_item_id=candidate.import_item_id
  AND validation.import_item_id=candidate.import_item_id
  AND validation.source_snapshot_id=candidate.expected_source_snapshot_id
  AND validation.target_platform_instance_id=candidate.target_platform_instance_id
)) THEN 'invalid review bulk approval item input'
ELSE '' END
FROM review_bulk_approval_items candidate
WHERE candidate.bulk_approval_id=? AND candidate.import_item_id=?`
