-- retrom: rebuild-with-foreign-keys-off
-- Pegasus server-source plans, automatic publication provenance, and VIDEO assets.

DROP TRIGGER review_multidisc_attachment_owner_insert;
DROP TRIGGER server_import_job_insert;

CREATE TABLE jobs_v28 (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE','SERVER_BIOS_IMPORT',
    'SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT'
  )),
  dedupe_key TEXT NOT NULL CHECK(length(dedupe_key)=64),
  execution_no INTEGER NOT NULL CHECK(execution_no>=1),
  payload_json TEXT NOT NULL,
  cancellable INTEGER NOT NULL CHECK(cancellable IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','CANCEL_REQUESTED','SUCCEEDED','FAILED','CANCELLED')),
  attempt_count INTEGER NOT NULL CHECK(attempt_count>=0),
  max_attempts INTEGER NOT NULL CHECK(max_attempts BETWEEN 1 AND 4),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  available_at_ms INTEGER NOT NULL CHECK(available_at_ms>=0),
  execution_started_at_ms INTEGER,
  execution_deadline_at_ms INTEGER,
  leased_until_ms INTEGER,
  heartbeat_at_ms INTEGER,
  finished_at_ms INTEGER,
  worker_id TEXT,
  error_code TEXT,
  error_retryable INTEGER CHECK(error_retryable IS NULL OR error_retryable IN (0,1)),
  cancel_requested_at_ms INTEGER,
  cancel_reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(kind,dedupe_key),
  CHECK((state IN ('SUCCEEDED','FAILED','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(kind NOT IN ('REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE') OR scope_type='IMPORT_ITEM'),
  CHECK(kind<>'SERVER_BIOS_IMPORT' OR scope_type='SERVER_IMPORT'),
  CHECK(kind NOT IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT') OR scope_type='PEGASUS_IMPORT')
);
INSERT INTO jobs_v28 SELECT * FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v28 RENAME TO jobs;
CREATE INDEX jobs_claim ON jobs(state,available_at_ms);
CREATE INDEX jobs_scope ON jobs(scope_type,scope_id);

CREATE TABLE job_events_v28 (
  id INTEGER PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id),
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  event_type TEXT NOT NULL CHECK(event_type IN (
    'QUEUED','STARTED','PROGRESS','RETRY_SCHEDULED','CANCEL_REQUESTED','MANUAL_RETRY',
    'ARCHIVE_SCANNED','PARENT_MATCHED','PARENT_REJECTED','SOURCE_SNAPSHOT_CREATED',
    'CORE_VALIDATION_COMPLETED','PLAYLIST_PARSED','DISC_SET_MATCHED','DISC_SET_REJECTED',
    'SUCCEEDED','FAILED','CANCELLED'
  )),
  data_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)
);
INSERT INTO job_events_v28 SELECT * FROM job_events;
DROP TABLE job_events;
ALTER TABLE job_events_v28 RENAME TO job_events;
CREATE INDEX job_events_scope ON job_events(scope_type,scope_id,id);
CREATE INDEX job_events_job ON job_events(job_id,id);
CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_multidisc_attachment_owner_insert
BEFORE INSERT ON review_multidisc_attachments
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.base_source_snapshot_id
  JOIN jobs job ON job.id=NEW.job_id
  WHERE draft.id=NEW.review_draft_id AND draft.import_item_id=NEW.import_item_id
  AND draft.effective_source_snapshot_id=NEW.base_source_snapshot_id
  AND snapshot.import_item_id=NEW.import_item_id AND snapshot.content_kind='MULTI_DISC_M3U_V1'
  AND job.scope_type='IMPORT_ITEM' AND job.scope_id=NEW.import_item_id
  AND job.kind='REVIEW_MULTI_DISC_VALIDATE'
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment owner'); END;

CREATE TRIGGER server_import_job_insert
BEFORE INSERT ON server_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.job_id AND job.kind='SERVER_BIOS_IMPORT'
  AND job.scope_type='SERVER_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid server import job'); END;

CREATE TABLE pegasus_imports (
  id TEXT PRIMARY KEY,
  root_id TEXT NOT NULL CHECK(length(CAST(root_id AS BLOB)) BETWEEN 1 AND 32),
  root_label_snapshot TEXT NOT NULL CHECK(length(root_label_snapshot) BETWEEN 1 AND 40 AND length(CAST(root_label_snapshot AS BLOB))<=160),
  source_relative_path TEXT NOT NULL CHECK(length(CAST(source_relative_path AS BLOB))<=4096),
  root_config_digest TEXT NOT NULL CHECK(length(root_config_digest)=64 AND root_config_digest=lower(root_config_digest)),
  source_snapshot_digest TEXT CHECK(source_snapshot_digest IS NULL OR (length(source_snapshot_digest)=64 AND source_snapshot_digest=lower(source_snapshot_digest))),
  state TEXT NOT NULL CHECK(state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED','EXPIRED')),
  phase TEXT CHECK(phase IS NULL OR phase IN ('DISCOVERING_METADATA','PARSING_METADATA','RESOLVING_SOURCES','COPYING_CONTENT','VALIDATING','PUBLISHING')),
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
  published_item_count INTEGER NOT NULL DEFAULT 0 CHECK(published_item_count>=0),
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
  CHECK(published_item_count+existing_item_count+blocked_item_count+failed_item_count+cancelled_item_count<=game_count)
);
CREATE UNIQUE INDEX pegasus_imports_one_active_execution ON pegasus_imports((1))
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');
CREATE INDEX pegasus_imports_history ON pegasus_imports(created_at_ms DESC,id DESC);
CREATE INDEX pegasus_imports_state ON pegasus_imports(state,updated_at_ms DESC,id DESC);

CREATE TABLE pegasus_import_metadata_files (
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  relative_path TEXT NOT NULL CHECK(length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 4096),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0 AND size_bytes<=8388608),
  content_digest TEXT NOT NULL CHECK(length(content_digest)=64 AND content_digest=lower(content_digest)),
  source_facts_digest TEXT NOT NULL CHECK(length(source_facts_digest)=64 AND source_facts_digest=lower(source_facts_digest)),
  parse_state TEXT NOT NULL CHECK(parse_state IN ('VALID','INVALID')),
  error_code TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_id,relative_path),
  CHECK((parse_state='INVALID')=(error_code IS NOT NULL))
);
CREATE INDEX pegasus_metadata_page ON pegasus_import_metadata_files(import_id,relative_path);
CREATE TRIGGER pegasus_metadata_files_immutable_update BEFORE UPDATE ON pegasus_import_metadata_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TABLE pegasus_import_collections (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  metadata_relative_path TEXT NOT NULL CHECK(length(CAST(metadata_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  segment_ordinal INTEGER NOT NULL CHECK(segment_ordinal>=0),
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
  shortname TEXT,
  description TEXT NOT NULL DEFAULT '',
  game_count INTEGER NOT NULL CHECK(game_count>=0),
  issue_count INTEGER NOT NULL DEFAULT 0 CHECK(issue_count>=0),
  ignored_rules_json TEXT NOT NULL DEFAULT '[]',
  warning_fields_json TEXT NOT NULL DEFAULT '[]',
  mapping_action TEXT CHECK(mapping_action IS NULL OR mapping_action IN ('IMPORT','SKIP')),
  target_platform_instance_id TEXT REFERENCES platform_instances(id),
  target_platform_instance_version INTEGER CHECK(target_platform_instance_version IS NULL OR target_platform_instance_version>=1),
  target_platform_id TEXT REFERENCES platforms(id),
  target_default_core_id TEXT REFERENCES cores(id),
  target_core_artifact_id TEXT REFERENCES core_artifacts(id),
  target_core_artifact_version INTEGER CHECK(target_core_artifact_version IS NULL OR target_core_artifact_version>=1),
  target_dat_version_id TEXT REFERENCES dat_versions(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(import_id,metadata_relative_path,segment_ordinal),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_version IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_default_core_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_core_artifact_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_core_artifact_version IS NOT NULL))
);
CREATE INDEX pegasus_collections_page ON pegasus_import_collections(import_id,metadata_relative_path,segment_ordinal,id);
CREATE INDEX pegasus_collections_mapping ON pegasus_import_collections(import_id,mapping_action,id);

CREATE TABLE pegasus_import_items (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  collection_id TEXT REFERENCES pegasus_import_collections(id),
  metadata_relative_path TEXT NOT NULL CHECK(length(CAST(metadata_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  game_ordinal INTEGER NOT NULL CHECK(game_ordinal>=0),
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key=lower(source_key)),
  title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  discovery_state TEXT NOT NULL CHECK(discovery_state IN ('READY','BLOCKED_SOURCE','BLOCKED_CONTENT')),
  execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','PUBLISHING','PUBLISHED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
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
  UNIQUE(import_id,source_key),
  UNIQUE(import_id,metadata_relative_path,game_ordinal),
  CHECK((execution_state IN ('PENDING','COPYING','VALIDATING','PUBLISHING'))=(completed_at_ms IS NULL)),
  CHECK((execution_state='PUBLISHED')=(published_game_id IS NOT NULL)),
  CHECK((execution_state='SKIPPED_EXISTING')=(existing_game_id IS NOT NULL AND existing_content_revision_id IS NOT NULL))
);
CREATE INDEX pegasus_items_page ON pegasus_import_items(import_id,title,id);
CREATE INDEX pegasus_items_outcome ON pegasus_import_items(import_id,execution_state,title,id);
CREATE INDEX pegasus_items_collection ON pegasus_import_items(import_id,collection_id,title,id);

CREATE TABLE pegasus_import_item_files (
  item_id TEXT NOT NULL REFERENCES pegasus_import_items(id),
  ordinal INTEGER NOT NULL CHECK(ordinal>=0 AND ordinal<64),
  declared_kind TEXT NOT NULL CHECK(declared_kind IN ('FILE','PLAYLIST','DISC')),
  relative_path TEXT NOT NULL CHECK(length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 4096),
  size_bytes INTEGER CHECK(size_bytes IS NULL OR size_bytes>=0),
  source_facts_digest TEXT CHECK(source_facts_digest IS NULL OR (length(source_facts_digest)=64 AND source_facts_digest=lower(source_facts_digest))),
  blob_id TEXT REFERENCES blobs(id),
  source_archive_blob_id TEXT REFERENCES blobs(id),
  source_archive_entry_ordinal INTEGER,
  role TEXT CHECK(role IS NULL OR role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT,
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','COPIED','SOURCE_CHANGED','READ_FAILED','UNSUPPORTED')),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(item_id,ordinal),
  UNIQUE(item_id,relative_path),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE pegasus_import_item_assets (
  item_id TEXT NOT NULL REFERENCES pegasus_import_items(id),
  kind TEXT NOT NULL CHECK(kind IN ('COVER','VIDEO')),
  resolution_method TEXT NOT NULL CHECK(resolution_method IN ('EXPLICIT_GAME','EXPLICIT_COLLECTION','AUTO_TITLE','AUTO_FILE')),
  relative_path TEXT NOT NULL CHECK(length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 4096),
  size_bytes INTEGER CHECK(size_bytes IS NULL OR size_bytes>=0),
  source_facts_digest TEXT CHECK(source_facts_digest IS NULL OR (length(source_facts_digest)=64 AND source_facts_digest=lower(source_facts_digest))),
  blob_id TEXT REFERENCES blobs(id),
  media_type TEXT,
  width_px INTEGER,
  height_px INTEGER,
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','COPIED','MISSING','AMBIGUOUS','INVALID','TOO_LARGE','SOURCE_CHANGED','READ_FAILED')),
  warning_code TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(item_id,kind),
  CHECK(kind<>'COVER' OR media_type IS NULL OR (media_type IN ('image/png','image/jpeg','image/webp') AND width_px>0 AND height_px>0)),
  CHECK(kind<>'VIDEO' OR media_type IS NULL OR (media_type IN ('video/mp4','video/webm') AND width_px IS NULL AND height_px IS NULL))
);

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
WHEN OLD.execution_state NOT IN ('COPYING','VALIDATING') OR NEW.execution_state NOT IN ('VALIDATING','PUBLISHING')
BEGIN SELECT RAISE(ABORT,'invalid Pegasus manifest transition'); END;

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

CREATE TRIGGER pegasus_file_snapshot_update
BEFORE UPDATE ON pegasus_import_item_files
WHEN NEW.item_id<>OLD.item_id OR NEW.ordinal<>OLD.ordinal OR NEW.declared_kind<>OLD.declared_kind OR
  NEW.relative_path<>OLD.relative_path OR NEW.size_bytes IS NOT OLD.size_bytes OR
  NEW.source_facts_digest IS NOT OLD.source_facts_digest OR NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus file snapshot'); END;

CREATE TRIGGER pegasus_asset_snapshot_update
BEFORE UPDATE ON pegasus_import_item_assets
WHEN NEW.item_id<>OLD.item_id OR NEW.kind<>OLD.kind OR NEW.resolution_method<>OLD.resolution_method OR
  NEW.relative_path<>OLD.relative_path OR NEW.size_bytes IS NOT OLD.size_bytes OR
  NEW.source_facts_digest IS NOT OLD.source_facts_digest OR NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus asset snapshot'); END;

DROP TRIGGER pegasus_item_published_update;
DROP TRIGGER games_current_metadata_owner_insert;
DROP TRIGGER games_current_owner_update;
DROP TRIGGER game_metadata_revisions_immutable_update;
DROP TRIGGER game_metadata_revisions_immutable_delete;
CREATE TABLE game_metadata_revisions_v28 (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  title TEXT NOT NULL CHECK(length(title)>0),
  description TEXT NOT NULL,
  developer TEXT NOT NULL,
  publisher TEXT NOT NULL,
  genre TEXT NOT NULL,
  players INTEGER CHECK(players IS NULL OR players BETWEEN 1 AND 64),
  release_year INTEGER,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_EDIT','RESCRAPE_APPLY','SERVER_PEGASUS_IMPORT')),
  source_ref_id TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(id,game_id),
  CHECK((source_kind='ADMIN_EDIT')=(source_ref_id IS NULL))
);
INSERT INTO game_metadata_revisions_v28 SELECT * FROM game_metadata_revisions;
DROP TABLE game_metadata_revisions;
ALTER TABLE game_metadata_revisions_v28 RENAME TO game_metadata_revisions;
CREATE INDEX fk_game_metadata_game ON game_metadata_revisions(game_id);
CREATE TRIGGER game_metadata_revisions_immutable_update BEFORE UPDATE ON game_metadata_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_metadata_revisions_immutable_delete BEFORE DELETE ON game_metadata_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_metadata_revisions_pegasus_source_insert
BEFORE INSERT ON game_metadata_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items item WHERE item.id=NEW.source_ref_id AND item.execution_state='PUBLISHING'
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus metadata source'); END;

DROP TRIGGER game_assets_immutable_update;
DROP TRIGGER game_assets_immutable_delete;
CREATE TABLE game_assets_v28 (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  metadata_revision_id TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  kind TEXT NOT NULL CHECK(kind IN ('COVER','BACKGROUND','SCREENSHOT','VIDEO')),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  width_px INTEGER,
  height_px INTEGER,
  media_type TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(metadata_revision_id,kind,ordinal),
  FOREIGN KEY(metadata_revision_id,game_id) REFERENCES game_metadata_revisions(id,game_id),
  CHECK(
    (kind IN ('COVER','BACKGROUND','SCREENSHOT') AND width_px>0 AND height_px>0 AND media_type IN ('image/png','image/jpeg','image/webp')) OR
    (kind='VIDEO' AND ordinal=0 AND width_px IS NULL AND height_px IS NULL AND media_type IN ('video/mp4','video/webm'))
  )
);
INSERT INTO game_assets_v28 SELECT * FROM game_assets;
DROP TABLE game_assets;
ALTER TABLE game_assets_v28 RENAME TO game_assets;
CREATE TRIGGER game_assets_immutable_update BEFORE UPDATE ON game_assets BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_assets_immutable_delete BEFORE DELETE ON game_assets BEGIN SELECT RAISE(ABORT,'immutable'); END;

DROP TRIGGER game_content_revisions_immutable_update;
DROP TRIGGER game_content_revisions_immutable_delete;
DROP TRIGGER game_content_revisions_review_snapshot_insert;
CREATE TABLE game_content_revisions_v28 (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' CHECK(content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_REPLACE','SERVER_PEGASUS_IMPORT')),
  source_ref_id TEXT NOT NULL,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(id,game_id)
);
INSERT INTO game_content_revisions_v28 SELECT * FROM game_content_revisions;
DROP TABLE game_content_revisions;
ALTER TABLE game_content_revisions_v28 RENAME TO game_content_revisions;
CREATE INDEX fk_game_content_game ON game_content_revisions(game_id);
CREATE TRIGGER game_content_revisions_immutable_update BEFORE UPDATE ON game_content_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_content_revisions_immutable_delete BEFORE DELETE ON game_content_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;
CREATE TRIGGER game_content_revisions_review_snapshot_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='IMPORT_REVIEW' AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=NEW.source_ref_id)
AND NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
  WHERE draft.import_item_id=NEW.source_ref_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
  AND snapshot.content_kind=NEW.content_kind
)
BEGIN SELECT RAISE(ABORT,'review content source snapshot mismatch'); END;
CREATE TRIGGER game_content_revisions_pegasus_source_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items item WHERE item.id=NEW.source_ref_id AND item.execution_state='PUBLISHING'
  AND item.source_manifest_digest=NEW.source_manifest_digest AND item.content_kind=NEW.content_kind
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus content source'); END;

CREATE TRIGGER games_current_metadata_owner_insert
BEFORE INSERT ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_metadata_revisions
    WHERE id=NEW.current_metadata_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_content_revisions
    WHERE id=NEW.current_content_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current content owner mismatch') END;
END;
CREATE TRIGGER games_current_owner_update
BEFORE UPDATE OF current_metadata_revision_id,current_content_revision_id ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_metadata_revisions
    WHERE id=NEW.current_metadata_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_content_revisions
    WHERE id=NEW.current_content_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current content owner mismatch') END;
END;

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
