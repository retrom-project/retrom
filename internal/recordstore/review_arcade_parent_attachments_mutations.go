package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewArcadeParentAttachments(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_arcade_parent_attachments",
		"id,state",
		ReviewArcadeParentAttachmentsUpdateRule,
	)
}

const ReviewArcadeParentAttachmentsUpdateRule = `
WITH previous(id,state) AS (VALUES(?,?))
SELECT CASE
-- review_arcade_parent_transition_update
WHEN ((candidate.state IS NOT previous.state) AND (NOT (
  previous.state='QUEUED' AND candidate.state IN ('RUNNING','CANCELLED','FAILED_RETRYABLE') OR
  previous.state='RUNNING' AND candidate.state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')
OR
  previous.state='FAILED_RETRYABLE' AND candidate.state IN ('QUEUED','RUNNING','CANCELLED')
))) THEN 'invalid attachment state transition'
ELSE '' END
FROM review_arcade_parent_attachments candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
