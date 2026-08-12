CREATE TABLE review_preview_sessions (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  actor_user_id TEXT NOT NULL REFERENCES users(id),
  idempotency_key TEXT NOT NULL,
  title TEXT NOT NULL CHECK(length(CAST(title AS BLOB)) BETWEEN 1 AND 800),
  content_kind TEXT NOT NULL CHECK(content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  content_blob_id TEXT NOT NULL REFERENCES blobs(id),
  content_logical_name TEXT NOT NULL CHECK(length(CAST(content_logical_name AS BLOB)) BETWEEN 1 AND 512),
  content_format TEXT NOT NULL CHECK(content_format IN ('SOURCE_V1','RETROM_DOS_DIRECT_ZIP_V1','RETROM_MULTIDISC_M3U_V1')),
  dependency_snapshot_json TEXT NOT NULL,
  default_dos_entry TEXT,
  emulator_game_id INTEGER NOT NULL CHECK(emulator_game_id>0),
  capture_allowed INTEGER NOT NULL CHECK(capture_allowed IN (0,1)),
  credential_sha256 BLOB NOT NULL CHECK(length(credential_sha256)=32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms INTEGER NOT NULL CHECK(bootstrap_expires_at_ms>=0),
  hard_expires_at_ms INTEGER NOT NULL CHECK(hard_expires_at_ms>=bootstrap_expires_at_ms),
  activated_at_ms INTEGER,
  finished_at_ms INTEGER,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  UNIQUE(actor_user_id,idempotency_key),
  CHECK(state!='ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('EXPIRED','REVOKED'))=(finished_at_ms IS NOT NULL))
);

CREATE INDEX review_preview_sessions_item
ON review_preview_sessions(import_item_id,created_at_ms DESC,id DESC);
CREATE INDEX review_preview_sessions_source ON review_preview_sessions(source_snapshot_id);
CREATE INDEX review_preview_sessions_validation ON review_preview_sessions(validation_id);
CREATE INDEX review_preview_sessions_target ON review_preview_sessions(target_platform_instance_id);
CREATE INDEX review_preview_sessions_artifact ON review_preview_sessions(core_artifact_id);
CREATE INDEX review_preview_sessions_actor ON review_preview_sessions(actor_user_id);

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
  JOIN users actor ON actor.id=NEW.actor_user_id AND actor.role='ADMIN' AND actor.status='ENABLED'
  WHERE item.id=NEW.import_item_id AND item.state='REVIEW_PENDING'
    AND draft.effective_source_snapshot_id=NEW.source_snapshot_id
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
) OR NEW.capture_allowed=1 AND NOT EXISTS (
  SELECT 1 FROM import_item_core_validations validation
  JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
    AND draft.selected_validation_id=validation.id
  WHERE validation.id=NEW.validation_id AND validation.status='READY'
)
BEGIN SELECT RAISE(ABORT,'invalid review preview snapshot'); END;

CREATE TABLE review_preview_files (
  preview_session_id TEXT NOT NULL REFERENCES review_preview_sessions(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','EXTERNAL_FILE','DISC')),
  logical_name TEXT NOT NULL CHECK(
    length(CAST(logical_name AS BLOB)) BETWEEN 1 AND 255 AND
    logical_name NOT LIKE '%/%' AND logical_name NOT LIKE '%\%' AND
    logical_name NOT IN ('.','..') AND instr(logical_name,char(0))=0
  ),
  virtual_path TEXT,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(preview_session_id,role,logical_name),
  UNIQUE(preview_session_id,virtual_path),
  CHECK(
    role IN ('PARENT','BIOS_BUNDLE') AND virtual_path IS NULL OR
    role IN ('EXTERNAL_FILE','DISC') AND virtual_path IS NOT NULL AND
      substr(virtual_path,1,1)='/' AND virtual_path NOT LIKE '%\%' AND
      virtual_path NOT LIKE '%?%' AND virtual_path NOT LIKE '%#%' AND
      instr(virtual_path,char(0))=0 AND virtual_path NOT LIKE '%//%' AND
      virtual_path NOT LIKE '%/./%' AND virtual_path NOT LIKE '%/../%' AND
      virtual_path NOT LIKE '%/.' AND virtual_path NOT LIKE '%/..'
  )
);

CREATE INDEX review_preview_files_blob ON review_preview_files(blob_id);

CREATE TRIGGER review_preview_files_immutable_update
BEFORE UPDATE ON review_preview_files BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER review_preview_files_immutable_delete
BEFORE DELETE ON review_preview_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE review_runtime_screenshots (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  preview_session_id TEXT NOT NULL REFERENCES review_preview_sessions(id),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  media_type TEXT NOT NULL CHECK(media_type='image/png'),
  width_px INTEGER NOT NULL CHECK(width_px BETWEEN 1 AND 40000000),
  height_px INTEGER NOT NULL CHECK(height_px BETWEEN 1 AND 40000000),
  captured_after_ms INTEGER NOT NULL CHECK(captured_after_ms=5000),
  captured_at_ms INTEGER NOT NULL CHECK(captured_at_ms>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=0),
  UNIQUE(import_item_id,validation_id)
);

CREATE INDEX review_runtime_screenshots_blob ON review_runtime_screenshots(blob_id);
CREATE INDEX review_runtime_screenshots_preview ON review_runtime_screenshots(preview_session_id);
CREATE INDEX review_runtime_screenshots_source ON review_runtime_screenshots(source_snapshot_id);
CREATE INDEX review_runtime_screenshots_validation ON review_runtime_screenshots(validation_id);
CREATE INDEX review_runtime_screenshots_artifact ON review_runtime_screenshots(core_artifact_id);

CREATE TRIGGER review_runtime_screenshots_validate_insert
BEFORE INSERT ON review_runtime_screenshots
WHEN NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN import_item_core_validations validation ON validation.id=preview.validation_id
    AND validation.status='READY'
  JOIN review_drafts draft ON draft.import_item_id=preview.import_item_id
    AND draft.effective_source_snapshot_id=preview.source_snapshot_id
    AND draft.selected_validation_id=preview.validation_id
  WHERE preview.id=NEW.preview_session_id AND preview.capture_allowed=1
    AND preview.import_item_id=NEW.import_item_id
    AND preview.source_snapshot_id=NEW.source_snapshot_id
    AND preview.validation_id=NEW.validation_id
    AND preview.core_artifact_id=NEW.core_artifact_id
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime screenshot'); END;

CREATE TRIGGER review_runtime_screenshots_validate_update
BEFORE UPDATE ON review_runtime_screenshots
WHEN NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN import_item_core_validations validation ON validation.id=preview.validation_id
    AND validation.status='READY'
  JOIN review_drafts draft ON draft.import_item_id=preview.import_item_id
    AND draft.effective_source_snapshot_id=preview.source_snapshot_id
    AND draft.selected_validation_id=preview.validation_id
  WHERE preview.id=NEW.preview_session_id AND preview.capture_allowed=1
    AND preview.import_item_id=NEW.import_item_id
    AND preview.source_snapshot_id=NEW.source_snapshot_id
    AND preview.validation_id=NEW.validation_id
    AND preview.core_artifact_id=NEW.core_artifact_id
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime screenshot'); END;
