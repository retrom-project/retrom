package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewBulkApprovalItems(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_bulk_approval_items",
		"bulk_approval_id,import_item_id,created_at_ms,expected_review_version,"+
			"expected_source_snapshot_id,expected_validation_id,game_id,ordinal,review_event_id,"+
			"state,target_platform_instance_id,target_platform_name_snapshot,title_snapshot",
		ReviewBulkApprovalItemsUpdateRule,
	)
}

const ReviewBulkApprovalItemsUpdateRule = `
WITH previous(bulk_approval_id,import_item_id,created_at_ms,expected_review_version,
expected_source_snapshot_id,expected_validation_id,game_id,ordinal,review_event_id,state,
target_platform_instance_id,target_platform_name_snapshot,title_snapshot) AS (VALUES(?,?,?,?,?,?,?,?,?,?,
?,?,?))
SELECT CASE
-- review_bulk_approval_items_frozen_update
WHEN ((candidate.bulk_approval_id IS NOT previous.bulk_approval_id OR candidate.import_item_id IS NOT
previous.import_item_id OR candidate.ordinal IS NOT previous.ordinal OR
candidate.expected_review_version IS NOT previous.expected_review_version OR
candidate.expected_validation_id IS NOT previous.expected_validation_id OR
candidate.expected_source_snapshot_id IS NOT previous.expected_source_snapshot_id OR
candidate.title_snapshot IS NOT previous.title_snapshot OR candidate.target_platform_instance_id IS NOT
previous.target_platform_instance_id OR candidate.target_platform_name_snapshot IS NOT
previous.target_platform_name_snapshot OR candidate.created_at_ms IS NOT previous.created_at_ms) AND
(1=1)) THEN 'immutable review bulk approval item input'
-- review_bulk_approval_items_published_update
WHEN ((candidate.state IS NOT previous.state OR candidate.game_id IS NOT previous.game_id OR
candidate.review_event_id IS NOT previous.review_event_id) AND (candidate.state='PUBLISHED' AND NOT
EXISTS(
  SELECT 1 FROM review_events event
  JOIN games game ON game.id=candidate.game_id
  WHERE event.id=candidate.review_event_id AND event.import_item_id=candidate.import_item_id
  AND event.event_type='APPROVED' AND json_extract(event.after_json,'$.gameId')=candidate.game_id
))) THEN 'invalid review bulk approval published result'
ELSE '' END
FROM review_bulk_approval_items candidate CROSS JOIN previous
WHERE candidate.bulk_approval_id=previous.bulk_approval_id AND
candidate.import_item_id=previous.import_item_id`
