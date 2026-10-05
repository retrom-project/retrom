package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateReviewArcadeParentAttachments(
	ctx context.Context, db dbapi.Executor, change Update,
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
WITH previous(id,state)
AS (VALUES(?::text,?::text))
SELECT CASE
-- review_arcade_parent_transition_update
WHEN ((candidate.state IS DISTINCT FROM previous.state) AND (NOT (
  previous.state='PENDING' AND candidate.state IN ('ACCEPTED','REJECTED','CANCELLED')
))) THEN 'invalid attachment state transition'
ELSE '' END
FROM review_arcade_parent_attachments candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
