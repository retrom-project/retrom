DROP TRIGGER review_preview_sessions_validate_insert;

CREATE TRIGGER review_preview_sessions_validate_insert
BEFORE INSERT ON review_preview_sessions
WHEN NOT EXISTS (
  SELECT 1 FROM import_items item
  JOIN review_drafts draft ON draft.import_item_id=item.id
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.source_snapshot_id
    AND snapshot.import_item_id=item.id
  JOIN import_item_core_validations validation ON validation.id=NEW.validation_id
    AND validation.import_item_id=item.id
    AND validation.source_snapshot_id=NEW.source_snapshot_id
    AND validation.target_platform_instance_id=NEW.target_platform_instance_id
    AND validation.core_artifact_id=NEW.core_artifact_id
    AND validation.prepublish_generation=4
  JOIN users actor ON actor.id=NEW.actor_user_id AND actor.role='ADMIN' AND actor.status='ENABLED'
  WHERE item.id=NEW.import_item_id AND item.state='REVIEW_PENDING'
    AND draft.effective_source_snapshot_id=NEW.source_snapshot_id
    AND draft.target_platform_instance_id=NEW.target_platform_instance_id
    AND validation.id=(
      SELECT candidate.id FROM import_item_core_validations candidate
      WHERE candidate.import_item_id=NEW.import_item_id
        AND candidate.source_snapshot_id=NEW.source_snapshot_id
        AND candidate.target_platform_instance_id=NEW.target_platform_instance_id
        AND candidate.core_artifact_id=NEW.core_artifact_id
      ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
    )
) OR NOT (
  NEW.content_kind='SINGLE_FILE' AND EXISTS (
    SELECT 1 FROM import_item_source_snapshot_files file
    WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='CONTENT'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='DOS_BUNDLE' AND EXISTS (
    SELECT 1 FROM import_item_validation_files file
    WHERE file.import_item_core_validation_id=NEW.validation_id AND file.role='DOS_LAUNCH_BUNDLE'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='MULTI_DISC_M3U_V1' AND EXISTS (
    SELECT 1 FROM import_item_validation_files file
    WHERE file.import_item_core_validation_id=NEW.validation_id AND file.role='MULTI_DISC_PLAYLIST'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  )
)
BEGIN SELECT RAISE(ABORT,'invalid review preview snapshot'); END;

DROP TRIGGER review_runtime_screenshots_validate_insert;
DROP TRIGGER review_runtime_screenshots_validate_update;

-- Preview sessions are short-lived, but a deployment may upgrade while an admin
-- already has one open. Make those existing sessions follow the same capture
-- policy as sessions created after this migration.
UPDATE review_preview_sessions SET capture_allowed=1 WHERE capture_allowed=0;

CREATE TRIGGER review_runtime_screenshots_validate_insert
BEFORE INSERT ON review_runtime_screenshots
WHEN NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN review_drafts draft ON draft.import_item_id=preview.import_item_id
    AND draft.effective_source_snapshot_id=preview.source_snapshot_id
    AND draft.target_platform_instance_id=preview.target_platform_instance_id
  JOIN import_item_core_validations validation ON validation.id=preview.validation_id
    AND validation.import_item_id=preview.import_item_id
    AND validation.source_snapshot_id=preview.source_snapshot_id
    AND validation.target_platform_instance_id=preview.target_platform_instance_id
    AND validation.core_artifact_id=preview.core_artifact_id
    AND validation.prepublish_generation=4
  WHERE preview.id=NEW.preview_session_id AND preview.capture_allowed=1
    AND preview.import_item_id=NEW.import_item_id
    AND preview.source_snapshot_id=NEW.source_snapshot_id
    AND preview.validation_id=NEW.validation_id
    AND preview.core_artifact_id=NEW.core_artifact_id
    AND validation.id=(
      SELECT candidate.id FROM import_item_core_validations candidate
      WHERE candidate.import_item_id=preview.import_item_id
        AND candidate.source_snapshot_id=preview.source_snapshot_id
        AND candidate.target_platform_instance_id=preview.target_platform_instance_id
        AND candidate.core_artifact_id=preview.core_artifact_id
      ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
    )
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime screenshot'); END;

CREATE TRIGGER review_runtime_screenshots_validate_update
BEFORE UPDATE ON review_runtime_screenshots
WHEN NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN review_drafts draft ON draft.import_item_id=preview.import_item_id
    AND draft.effective_source_snapshot_id=preview.source_snapshot_id
    AND draft.target_platform_instance_id=preview.target_platform_instance_id
  JOIN import_item_core_validations validation ON validation.id=preview.validation_id
    AND validation.import_item_id=preview.import_item_id
    AND validation.source_snapshot_id=preview.source_snapshot_id
    AND validation.target_platform_instance_id=preview.target_platform_instance_id
    AND validation.core_artifact_id=preview.core_artifact_id
    AND validation.prepublish_generation=4
  WHERE preview.id=NEW.preview_session_id AND preview.capture_allowed=1
    AND preview.import_item_id=NEW.import_item_id
    AND preview.source_snapshot_id=NEW.source_snapshot_id
    AND preview.validation_id=NEW.validation_id
    AND preview.core_artifact_id=NEW.core_artifact_id
    AND validation.id=(
      SELECT candidate.id FROM import_item_core_validations candidate
      WHERE candidate.import_item_id=preview.import_item_id
        AND candidate.source_snapshot_id=preview.source_snapshot_id
        AND candidate.target_platform_instance_id=preview.target_platform_instance_id
        AND candidate.core_artifact_id=preview.core_artifact_id
      ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
    )
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime screenshot'); END;
