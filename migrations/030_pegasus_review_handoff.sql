-- retrom: rebuild-with-foreign-keys-off
-- Pegasus processing prepares ordinary review items; only an explicit review
-- decision may publish or discard the linked game candidate.

DROP TRIGGER game_metadata_revisions_pegasus_source_insert;
DROP TRIGGER game_content_revisions_pegasus_source_insert;
DROP TRIGGER pegasus_collection_mapping_update;

CREATE TABLE pegasus_imports_v30 (
  id TEXT PRIMARY KEY,
  root_id TEXT NOT NULL CHECK(length(CAST(root_id AS BLOB)) BETWEEN 1 AND 32),
  root_label_snapshot TEXT NOT NULL CHECK(length(root_label_snapshot) BETWEEN 1 AND 40 AND length(CAST(root_label_snapshot AS BLOB))<=160),
  source_relative_path TEXT NOT NULL CHECK(length(CAST(source_relative_path AS BLOB))<=4096),
  root_config_digest TEXT NOT NULL CHECK(length(root_config_digest)=64 AND root_config_digest=lower(root_config_digest)),
  source_snapshot_digest TEXT CHECK(source_snapshot_digest IS NULL OR (length(source_snapshot_digest)=64 AND source_snapshot_digest=lower(source_snapshot_digest))),
  state TEXT NOT NULL CHECK(state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED','EXPIRED')),
  phase TEXT CHECK(phase IS NULL OR phase IN ('DISCOVERING_METADATA','PARSING_METADATA','RESOLVING_SOURCES','COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS','PUBLISHING')),
  scan_job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  import_job_id TEXT UNIQUE REFERENCES jobs(id),
  metadata_count INTEGER NOT NULL DEFAULT 0 CHECK(metadata_count>=0),
  invalid_metadata_count INTEGER NOT NULL DEFAULT 0 CHECK(invalid_metadata_count>=0),
  collection_count INTEGER NOT NULL DEFAULT 0 CHECK(collection_count>=0),
  game_count INTEGER NOT NULL DEFAULT 0 CHECK(game_count>=0),
  estimated_source_bytes INTEGER NOT NULL DEFAULT 0 CHECK(estimated_source_bytes>=0),
  mapped_collection_count INTEGER NOT NULL DEFAULT 0 CHECK(mapped_collection_count>=0),
  skipped_collection_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_collection_count>=0),
  processable_item_count INTEGER NOT NULL DEFAULT 0 CHECK(processable_item_count>=0),
  blocked_item_count INTEGER NOT NULL DEFAULT 0 CHECK(blocked_item_count>=0),
  review_pending_item_count INTEGER NOT NULL DEFAULT 0 CHECK(review_pending_item_count>=0),
  published_item_count INTEGER NOT NULL DEFAULT 0 CHECK(published_item_count>=0),
  review_discarded_item_count INTEGER NOT NULL DEFAULT 0 CHECK(review_discarded_item_count>=0),
  existing_item_count INTEGER NOT NULL DEFAULT 0 CHECK(existing_item_count>=0),
  failed_item_count INTEGER NOT NULL DEFAULT 0 CHECK(failed_item_count>=0),
  cancelled_item_count INTEGER NOT NULL DEFAULT 0 CHECK(cancelled_item_count>=0),
  media_warning_count INTEGER NOT NULL DEFAULT 0 CHECK(media_warning_count>=0),
  discovered_cover_count INTEGER NOT NULL DEFAULT 0 CHECK(discovered_cover_count>=0),
  discovered_video_count INTEGER NOT NULL DEFAULT 0 CHECK(discovered_video_count>=0),
  mapping_version INTEGER NOT NULL DEFAULT 1 CHECK(mapping_version>=1),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  last_error_code TEXT,
  retryable INTEGER NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  cancel_reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  scan_completed_at_ms INTEGER,
  started_at_ms INTEGER,
  completed_at_ms INTEGER,
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=created_at_ms),
  CHECK((state IN ('PARTIAL_FAILURE','COMPLETED','CANCELLED','FAILED','EXPIRED'))=(completed_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_reason IS NOT NULL)),
  CHECK(mapped_collection_count+skipped_collection_count<=collection_count),
  CHECK(review_pending_item_count+published_item_count+review_discarded_item_count+existing_item_count+blocked_item_count+failed_item_count+cancelled_item_count<=game_count)
);

INSERT INTO pegasus_imports_v30(
  id,root_id,root_label_snapshot,source_relative_path,root_config_digest,source_snapshot_digest,
  state,phase,scan_job_id,import_job_id,metadata_count,invalid_metadata_count,collection_count,
  game_count,estimated_source_bytes,mapped_collection_count,skipped_collection_count,
  processable_item_count,blocked_item_count,review_pending_item_count,published_item_count,
  review_discarded_item_count,existing_item_count,failed_item_count,cancelled_item_count,
  media_warning_count,discovered_cover_count,discovered_video_count,mapping_version,version,
  created_by_user_id,last_error_code,retryable,cancel_reason,created_at_ms,updated_at_ms,
  scan_completed_at_ms,started_at_ms,completed_at_ms,expires_at_ms
)
SELECT id,root_id,root_label_snapshot,source_relative_path,root_config_digest,source_snapshot_digest,
  state,phase,scan_job_id,import_job_id,metadata_count,invalid_metadata_count,collection_count,
  game_count,estimated_source_bytes,mapped_collection_count,skipped_collection_count,
  processable_item_count,blocked_item_count,0,published_item_count,0,existing_item_count,
  failed_item_count,cancelled_item_count,media_warning_count,discovered_cover_count,
  discovered_video_count,mapping_version,version,created_by_user_id,last_error_code,retryable,
  cancel_reason,created_at_ms,updated_at_ms,scan_completed_at_ms,started_at_ms,completed_at_ms,
  expires_at_ms
FROM pegasus_imports;

DROP TABLE pegasus_imports;
ALTER TABLE pegasus_imports_v30 RENAME TO pegasus_imports;
CREATE UNIQUE INDEX pegasus_imports_one_active_execution ON pegasus_imports((1))
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');
CREATE INDEX pegasus_imports_history ON pegasus_imports(created_at_ms DESC,id DESC);
CREATE INDEX pegasus_imports_state ON pegasus_imports(state,updated_at_ms DESC,id DESC);

CREATE TRIGGER pegasus_import_scan_job_insert
BEFORE INSERT ON pegasus_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.scan_job_id AND job.kind='SERVER_PEGASUS_SCAN'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus scan job'); END;

CREATE TRIGGER pegasus_import_job_update
BEFORE UPDATE OF import_job_id ON pegasus_imports
WHEN NEW.import_job_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.import_job_id AND job.kind='SERVER_PEGASUS_IMPORT'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus import job'); END;

CREATE TRIGGER pegasus_collection_mapping_update
BEFORE UPDATE OF mapping_action,target_platform_instance_id,target_platform_instance_version,target_platform_id,
  target_default_core_id,target_core_artifact_id,target_core_artifact_version,target_dat_version_id
ON pegasus_import_collections
WHEN NOT EXISTS(SELECT 1 FROM pegasus_imports import WHERE import.id=OLD.import_id AND import.state='AWAITING_MAPPING')
BEGIN SELECT RAISE(ABORT,'Pegasus mapping is frozen'); END;

CREATE TABLE pegasus_import_items_v30 (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  collection_id TEXT REFERENCES pegasus_import_collections(id),
  metadata_relative_path TEXT NOT NULL CHECK(length(CAST(metadata_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  game_ordinal INTEGER NOT NULL CHECK(game_ordinal>=0),
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key=lower(source_key)),
  title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  discovery_state TEXT NOT NULL CHECK(discovery_state IN ('READY','BLOCKED_SOURCE','BLOCKED_CONTENT')),
  execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','PUBLISHING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  content_kind TEXT CHECK(content_kind IS NULL OR content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  metadata_json TEXT NOT NULL,
  warnings_json TEXT NOT NULL DEFAULT '[]',
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  discovery_code TEXT,
  error_code TEXT,
  retryable INTEGER NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  library_import_job_id TEXT REFERENCES import_jobs(id),
  library_import_item_id TEXT REFERENCES import_items(id),
  published_game_id TEXT REFERENCES games(id),
  existing_game_id TEXT REFERENCES games(id),
  existing_content_revision_id TEXT REFERENCES game_content_revisions(id),
  existing_matches_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(existing_matches_json) AND json_type(existing_matches_json)='array'),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  error_details_json TEXT CHECK(
    error_details_json IS NULL OR (
      json_valid(error_details_json) AND json_type(error_details_json)='object'
      AND length(CAST(error_details_json AS BLOB))<=8192
    )
  ),
  UNIQUE(import_id,source_key),
  UNIQUE(import_id,metadata_relative_path,game_ordinal),
  CHECK((execution_state IN ('PENDING','COPYING','VALIDATING','PUBLISHING'))=(completed_at_ms IS NULL)),
  CHECK((execution_state='PUBLISHED')=(published_game_id IS NOT NULL)),
  CHECK((execution_state='SKIPPED_EXISTING')=(existing_game_id IS NOT NULL AND existing_content_revision_id IS NOT NULL))
);

INSERT INTO pegasus_import_items_v30(
  id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,
  execution_state,content_kind,metadata_json,warnings_json,source_manifest_json,
  source_manifest_digest,discovery_code,error_code,retryable,library_import_job_id,
  library_import_item_id,published_game_id,existing_game_id,existing_content_revision_id,
  existing_matches_json,created_at_ms,updated_at_ms,completed_at_ms,error_details_json
)
SELECT id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,
  execution_state,content_kind,metadata_json,warnings_json,source_manifest_json,
  source_manifest_digest,discovery_code,error_code,retryable,library_import_job_id,
  library_import_item_id,published_game_id,existing_game_id,existing_content_revision_id,
  existing_matches_json,created_at_ms,updated_at_ms,completed_at_ms,error_details_json
FROM pegasus_import_items;

DROP TABLE pegasus_import_items;
ALTER TABLE pegasus_import_items_v30 RENAME TO pegasus_import_items;
CREATE INDEX pegasus_items_page ON pegasus_import_items(import_id,title,id);
CREATE INDEX pegasus_items_outcome ON pegasus_import_items(import_id,execution_state,title,id);
CREATE INDEX pegasus_items_collection ON pegasus_import_items(import_id,collection_id,title,id);
CREATE UNIQUE INDEX pegasus_items_library_review ON pegasus_import_items(library_import_item_id)
WHERE library_import_item_id IS NOT NULL;

CREATE TRIGGER pegasus_item_snapshot_update
BEFORE UPDATE ON pegasus_import_items
WHEN NEW.import_id<>OLD.import_id OR NEW.collection_id IS NOT OLD.collection_id OR
  NEW.metadata_relative_path<>OLD.metadata_relative_path OR NEW.game_ordinal<>OLD.game_ordinal OR
  NEW.source_key<>OLD.source_key OR NEW.title<>OLD.title OR NEW.discovery_state<>OLD.discovery_state OR
  NEW.metadata_json<>OLD.metadata_json OR NEW.discovery_code IS NOT OLD.discovery_code OR
  NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus item snapshot'); END;

CREATE TRIGGER pegasus_item_manifest_update
BEFORE UPDATE OF content_kind,source_manifest_json,source_manifest_digest,library_import_job_id,library_import_item_id
ON pegasus_import_items
WHEN OLD.execution_state NOT IN ('COPYING','VALIDATING') OR NEW.execution_state NOT IN ('VALIDATING','PUBLISHING','REVIEW_PENDING')
BEGIN SELECT RAISE(ABORT,'invalid Pegasus manifest transition'); END;

CREATE TRIGGER pegasus_item_review_pending_update
BEFORE UPDATE OF execution_state ON pegasus_import_items
WHEN NEW.execution_state='REVIEW_PENDING' AND (
  NEW.library_import_job_id IS NULL OR NEW.library_import_item_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM import_items item
    WHERE item.id=NEW.library_import_item_id AND item.import_job_id=NEW.library_import_job_id
    AND item.state='REVIEW_PENDING'
  )
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus review handoff'); END;

CREATE TRIGGER pegasus_item_review_discarded_update
BEFORE UPDATE OF execution_state ON pegasus_import_items
WHEN NEW.execution_state='REVIEW_DISCARDED' AND NOT EXISTS(
  SELECT 1 FROM import_items item WHERE item.id=NEW.library_import_item_id AND item.state='DISCARDED'
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus review discard'); END;

CREATE TRIGGER pegasus_item_published_update
BEFORE UPDATE OF execution_state,published_game_id ON pegasus_import_items
WHEN NEW.execution_state='PUBLISHED' AND (
  NEW.published_game_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM games game
    JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
    JOIN game_content_revisions content ON content.id=game.current_content_revision_id
    WHERE game.id=NEW.published_game_id AND metadata.source_kind='SERVER_PEGASUS_IMPORT'
    AND metadata.source_ref_id=NEW.id AND content.source_kind='SERVER_PEGASUS_IMPORT'
    AND content.source_ref_id=NEW.id
  )
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus published game'); END;

CREATE TRIGGER game_metadata_revisions_pegasus_source_insert
BEFORE INSERT ON game_metadata_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items item WHERE item.id=NEW.source_ref_id
  AND item.execution_state IN ('PUBLISHING','REVIEW_PENDING')
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus metadata source'); END;

CREATE TRIGGER game_content_revisions_pegasus_source_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items item WHERE item.id=NEW.source_ref_id
  AND item.execution_state IN ('PUBLISHING','REVIEW_PENDING')
  AND item.source_manifest_digest=NEW.source_manifest_digest AND item.content_kind=NEW.content_kind
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus content source'); END;
