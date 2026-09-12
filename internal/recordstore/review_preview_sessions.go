package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewPreviewSessions(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewPreviewSessions)
}

func ValidateReviewPreviewSessions(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_preview_sessionsOwnership, keys)
}

const review_preview_sessionsOwnership = `
SELECT CASE
WHEN (candidate.restore_from_preview_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM review_preview_sessions source
 WHERE source.id=candidate.restore_from_preview_id AND source.id<>candidate.id AND
source.actor_user_id=candidate.actor_user_id
 AND source.import_item_id=candidate.import_item_id AND
source.source_snapshot_id=candidate.source_snapshot_id
 AND source.provider_id=candidate.provider_id AND source.target_id=candidate.target_id
 AND source.checkpoint_payload_blob_id=candidate.restore_payload_blob_id
 AND source.checkpoint_format=candidate.restore_checkpoint_format
 AND source.state IN ('ACTIVE','FINISHED') AND source.hard_expires_at_ms>candidate.created_at_ms
)) THEN 'invalid review restore source'
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
    AND provider.bundle_sha256=candidate.bundle_sha256
)
OR NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
  JOIN import_items item ON item.id=draft.import_item_id
  WHERE validation.id=candidate.validation_id AND validation.import_item_id=candidate.import_item_id
    AND validation.source_snapshot_id=candidate.source_snapshot_id
    AND validation.provider_id=candidate.provider_id AND validation.target_id=candidate.target_id
    AND draft.effective_source_snapshot_id=candidate.source_snapshot_id
    AND draft.target_platform_instance_id=candidate.target_platform_instance_id
    AND validation.target_platform_instance_id=candidate.target_platform_instance_id
    AND item.state='REVIEW_PENDING' AND item.payload_state='RETAINED'
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM review_preview_sessions candidate
WHERE candidate.id=?`
