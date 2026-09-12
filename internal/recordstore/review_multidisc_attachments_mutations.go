package recordstore

import (
	"context"
	"database/sql"
)

func UpdateReviewMultidiscAttachments(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"review_multidisc_attachments",
		"id,base_source_snapshot_id,created_at_ms,expected_set_digest,import_item_id,job_id,"+
			"requested_by_user_id,result_source_snapshot_id,result_validation_id,review_draft_id,"+
			"state,upload_session_id",
		ReviewMultidiscAttachmentsUpdateRule,
	)
}

const ReviewMultidiscAttachmentsUpdateRule = `
WITH previous(id,base_source_snapshot_id,created_at_ms,expected_set_digest,import_item_id,job_id,
requested_by_user_id,result_source_snapshot_id,result_validation_id,review_draft_id,state,
upload_session_id) AS (VALUES(?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- review_multidisc_attachment_identity_update
WHEN ((candidate.import_item_id IS NOT previous.import_item_id OR candidate.review_draft_id IS NOT
previous.review_draft_id OR candidate.requested_by_user_id IS NOT previous.requested_by_user_id OR
candidate.base_source_snapshot_id IS NOT previous.base_source_snapshot_id OR candidate.upload_session_id
IS NOT previous.upload_session_id OR candidate.expected_set_digest IS NOT previous.expected_set_digest
OR candidate.job_id IS NOT previous.job_id OR candidate.created_at_ms IS NOT previous.created_at_ms) AND
(1=1)) THEN 'multi-disc attachment identity is immutable'
-- review_multidisc_attachment_result_update
WHEN ((candidate.state IS NOT previous.state OR candidate.result_source_snapshot_id IS NOT
previous.result_source_snapshot_id OR candidate.result_validation_id IS NOT
previous.result_validation_id) AND (candidate.state='ACCEPTED' AND NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  JOIN import_item_core_validations validation ON validation.id=candidate.result_validation_id
  WHERE snapshot.id=candidate.result_source_snapshot_id AND
snapshot.import_item_id=candidate.import_item_id
  AND snapshot.content_kind='MULTI_DISC' AND snapshot.created_by='MULTI_DISC_ATTACHMENT'
  AND snapshot.id<>candidate.base_source_snapshot_id
  AND validation.import_item_id=candidate.import_item_id
  AND validation.source_snapshot_id=candidate.result_source_snapshot_id
))) THEN 'invalid multi-disc attachment result'
-- review_multidisc_attachment_terminal_update
WHEN (previous.state IN ('ACCEPTED','REJECTED','CANCELLED')) THEN
'terminal multi-disc attachment is immutable'
-- review_multidisc_attachment_transition_update
WHEN ((candidate.state IS NOT previous.state) AND (NOT (
  previous.state='QUEUED' AND candidate.state IN ('RUNNING','CANCELLED') OR
  previous.state='RUNNING' AND candidate.state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')
OR
  previous.state='FAILED_RETRYABLE' AND candidate.state IN ('RUNNING','CANCELLED')
))) THEN 'invalid multi-disc attachment state transition'
ELSE '' END
FROM review_multidisc_attachments candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteReviewMultidiscAttachments(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"review_multidisc_attachments",
		"id",
		ReviewMultidiscAttachmentsDeleteRule,
	)
}

const ReviewMultidiscAttachmentsDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- review_multidisc_attachment_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
