package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewBulkApprovals(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewBulkApprovals)
}

func ValidateReviewBulkApprovals(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_bulk_approvalsOwnership, keys)
}

const review_bulk_approvalsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.job_id AND job.kind='REVIEW_BULK_APPROVE'
  AND job.scope_type='REVIEW_BULK_APPROVAL' AND job.scope_id=candidate.id
)) THEN 'invalid review bulk approval job'
ELSE '' END
FROM review_bulk_approvals candidate
WHERE candidate.id=?`
