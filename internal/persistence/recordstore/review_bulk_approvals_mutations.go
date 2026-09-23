package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateReviewBulkApprovals(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(ctx, db, change, "review_bulk_approvals",
		"id,job_id,max_item_id,initial_pending_count,created_by_user_id,created_at_ms",
		ReviewBulkApprovalsUpdateRule)
}

const ReviewBulkApprovalsUpdateRule = `
WITH previous(id,job_id,max_item_id,initial_pending_count,created_by_user_id,created_at_ms)
AS (VALUES(?,?,?,?,?,?))
SELECT CASE WHEN (
    candidate.job_id IS NOT previous.job_id OR
    candidate.max_item_id IS NOT previous.max_item_id OR
    candidate.initial_pending_count IS NOT previous.initial_pending_count OR
    candidate.created_by_user_id IS NOT previous.created_by_user_id OR
    candidate.created_at_ms IS NOT previous.created_at_ms
) THEN 'immutable review bulk approval input' ELSE '' END
FROM review_bulk_approvals candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
