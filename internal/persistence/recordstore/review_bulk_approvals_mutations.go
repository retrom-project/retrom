package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateReviewBulkApprovals(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(ctx, db, change, "review_bulk_approvals",
		"id,job_id,max_item_id,initial_pending_count,created_by_user_id,created_at_ms",
		ReviewBulkApprovalsUpdateRule)
}

const ReviewBulkApprovalsUpdateRule = `
WITH previous(id,job_id,max_item_id,initial_pending_count,created_by_user_id,created_at_ms)
AS (VALUES(?::text,?::text,?::text,?::bigint,?::text,?::bigint))
SELECT CASE WHEN (
    candidate.job_id IS DISTINCT FROM previous.job_id OR
    candidate.max_item_id IS DISTINCT FROM previous.max_item_id OR
    candidate.initial_pending_count IS DISTINCT FROM previous.initial_pending_count OR
    candidate.created_by_user_id IS DISTINCT FROM previous.created_by_user_id OR
    candidate.created_at_ms IS DISTINCT FROM previous.created_at_ms
) THEN 'immutable review bulk approval input' ELSE '' END
FROM review_bulk_approvals candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
