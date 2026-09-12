package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewBulkApprovals(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_bulk_approvals",
		"id,attachment_active_count,candidate_count,candidate_manifest_digest,created_at_ms,"+
			"created_by_user_id,duplicate_count,job_id,matched_count,scope_digest,scope_json,"+
			"screenshot_only_count,source_flagged_count",
		ReviewBulkApprovalsUpdateRule,
	)
}

const ReviewBulkApprovalsUpdateRule = `
WITH previous(id,attachment_active_count,candidate_count,candidate_manifest_digest,created_at_ms,
created_by_user_id,duplicate_count,job_id,matched_count,scope_digest,scope_json,screenshot_only_count,
source_flagged_count) AS (VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- review_bulk_approvals_frozen_update
WHEN ((candidate.job_id IS NOT previous.job_id OR candidate.scope_json IS NOT previous.scope_json OR
candidate.scope_digest IS NOT previous.scope_digest OR candidate.candidate_manifest_digest IS NOT
previous.candidate_manifest_digest OR candidate.matched_count IS NOT previous.matched_count OR
candidate.candidate_count IS NOT previous.candidate_count OR candidate.screenshot_only_count IS NOT
previous.screenshot_only_count OR candidate.duplicate_count IS NOT previous.duplicate_count OR
candidate.attachment_active_count IS NOT previous.attachment_active_count OR
candidate.source_flagged_count IS NOT previous.source_flagged_count OR candidate.created_by_user_id IS
NOT previous.created_by_user_id OR candidate.created_at_ms IS NOT previous.created_at_ms) AND (1=1))
THEN 'immutable review bulk approval input'
ELSE '' END
FROM review_bulk_approvals candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
