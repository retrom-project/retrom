-- Clean pre-release baseline: server_import.

CREATE TABLE "pegasus_imports" (
  id TEXT PRIMARY KEY,
  root_id TEXT NOT NULL CHECK(length(CAST(root_id AS BLOB)) BETWEEN 1 AND 32),
  root_label_snapshot TEXT NOT NULL CHECK(length(root_label_snapshot) BETWEEN 1 AND 40 AND length(CAST(root_label_snapshot AS BLOB))<=160),
  source_relative_path TEXT NOT NULL CHECK(length(CAST(source_relative_path AS BLOB))<=4096),
  root_config_digest TEXT NOT NULL CHECK(length(root_config_digest)=64 AND root_config_digest=lower(root_config_digest)),
  source_snapshot_digest TEXT CHECK(source_snapshot_digest IS NULL OR (length(source_snapshot_digest)=64 AND source_snapshot_digest=lower(source_snapshot_digest))),
  state TEXT NOT NULL CHECK(state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED','EXPIRED')),
  phase TEXT CHECK(phase IS NULL OR phase IN ('DISCOVERING_METADATA','PARSING_METADATA','RESOLVING_SOURCES','COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS')),
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
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms), tag_snapshot_json TEXT NOT NULL DEFAULT '[]'
CHECK(json_valid(tag_snapshot_json) AND json_type(tag_snapshot_json)='array'),
  UNIQUE(import_id,metadata_relative_path,segment_ordinal),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_version IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_default_core_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_core_artifact_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_core_artifact_version IS NOT NULL))
);

CREATE TABLE pegasus_collection_tags (
  collection_id TEXT NOT NULL REFERENCES pegasus_import_collections(id),
  tag_id TEXT NOT NULL REFERENCES tags(id),
  assigned_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(collection_id,tag_id)
);

CREATE TABLE "pegasus_import_items" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  collection_id TEXT REFERENCES pegasus_import_collections(id),
  metadata_relative_path TEXT NOT NULL CHECK(length(CAST(metadata_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  game_ordinal INTEGER NOT NULL CHECK(game_ordinal>=0),
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key=lower(source_key)),
  title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  discovery_state TEXT NOT NULL CHECK(discovery_state IN ('READY','BLOCKED_SOURCE','BLOCKED_CONTENT')),
  execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  content_kind TEXT CHECK(content_kind IS NULL OR content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  metadata_json TEXT NOT NULL,
  warnings_json TEXT NOT NULL DEFAULT '[]',
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  discovery_code TEXT,
  error_code TEXT,
  retryable INTEGER NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED','FAILED')),
  payload_release_job_id TEXT REFERENCES jobs(id),
  payload_released_at_ms INTEGER,
  payload_last_error_code TEXT,
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
  CHECK((execution_state IN ('PENDING','COPYING','VALIDATING'))=(completed_at_ms IS NULL)),
  CHECK((execution_state='PUBLISHED')=(published_game_id IS NOT NULL)),
  CHECK((execution_state='SKIPPED_EXISTING')=(existing_game_id IS NOT NULL AND existing_content_revision_id IS NOT NULL)),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL AND payload_last_error_code IS NULL OR
    payload_state='FAILED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NOT NULL
  )
);

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
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','COPIED','SOURCE_CHANGED','READ_FAILED','UNSUPPORTED','PAYLOAD_RELEASED')),
  payload_released_at_ms INTEGER,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(item_id,ordinal),
  UNIQUE(item_id,relative_path),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL)),
  CHECK(state='PAYLOAD_RELEASED' AND blob_id IS NULL AND source_archive_blob_id IS NULL AND payload_released_at_ms IS NOT NULL OR
        state<>'PAYLOAD_RELEASED' AND payload_released_at_ms IS NULL)
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
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','COPIED','MISSING','AMBIGUOUS','INVALID','TOO_LARGE','SOURCE_CHANGED','READ_FAILED','PAYLOAD_RELEASED')),
  payload_released_at_ms INTEGER,
  warning_code TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(item_id,kind),
  CHECK(kind<>'COVER' OR media_type IS NULL OR (media_type IN ('image/png','image/jpeg','image/webp') AND width_px>0 AND height_px>0)),
  CHECK(kind<>'VIDEO' OR media_type IS NULL OR (media_type IN ('video/mp4','video/webm') AND width_px IS NULL AND height_px IS NULL)),
  CHECK(state='PAYLOAD_RELEASED' AND blob_id IS NULL AND payload_released_at_ms IS NOT NULL OR
        state<>'PAYLOAD_RELEASED' AND payload_released_at_ms IS NULL)
);
