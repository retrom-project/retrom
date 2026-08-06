CREATE TABLE import_jobs (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL UNIQUE REFERENCES upload_sessions(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  platform_instance_version INTEGER NOT NULL,
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  default_core_id TEXT NOT NULL REFERENCES cores(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  dat_version_id TEXT REFERENCES dat_versions(id),
  metadata_provider TEXT NOT NULL CHECK(metadata_provider IN ('HASHEOUS','NONE')),
  config_snapshot_json TEXT NOT NULL,
  config_snapshot_digest TEXT NOT NULL CHECK(length(config_snapshot_digest) = 64),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','REVIEW_PENDING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED')),
  total_item_count INTEGER NOT NULL DEFAULT 0,
  queued_item_count INTEGER NOT NULL DEFAULT 0,
  running_item_count INTEGER NOT NULL DEFAULT 0,
  review_pending_item_count INTEGER NOT NULL DEFAULT 0,
  published_item_count INTEGER NOT NULL DEFAULT 0,
  discarded_item_count INTEGER NOT NULL DEFAULT 0,
  failed_item_count INTEGER NOT NULL DEFAULT 0,
  cancelled_item_count INTEGER NOT NULL DEFAULT 0,
  ignored_file_count INTEGER NOT NULL DEFAULT 0,
  rejected_file_count INTEGER NOT NULL DEFAULT 0,
  last_error_code TEXT,
  cancel_requested_at_ms INTEGER,
  cancel_reason TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  completed_at_ms INTEGER,
  CHECK(total_item_count = queued_item_count + running_item_count + review_pending_item_count + published_item_count + discarded_item_count + failed_item_count + cancelled_item_count)
);

CREATE TABLE import_job_files (
  import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  disposition TEXT NOT NULL CHECK(disposition IN ('PENDING','SOURCE','IGNORED','REJECTED')),
  reason_code TEXT,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_job_id, upload_file_id),
  CHECK((disposition IN ('IGNORED','REJECTED')) = (reason_code IS NOT NULL))
);

CREATE TABLE import_items (
  id TEXT PRIMARY KEY,
  import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  group_key TEXT NOT NULL CHECK(length(group_key) = 64),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','HASHING','IDENTIFYING','SCRAPING','REVIEW_PENDING','PUBLISHED','DISCARDED','FAILED_RETRYABLE','FAILED_FINAL','CANCELLED')),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest) = 64),
  search_text TEXT NOT NULL,
  failed_stage TEXT CHECK(failed_stage IS NULL OR failed_stage IN ('HASHING','IDENTIFYING','SCRAPING')),
  last_error_code TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  completed_at_ms INTEGER,
  UNIQUE(import_job_id, group_key),
  CHECK((state IN ('FAILED_RETRYABLE','FAILED_FINAL')) = (failed_stage IS NOT NULL AND last_error_code IS NOT NULL))
);

CREATE TABLE import_item_source_files (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL,
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_item_id, role, logical_name),
  FOREIGN KEY(source_archive_blob_id, source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id, ordinal),
  CHECK((source_archive_blob_id IS NULL) = (source_archive_entry_ordinal IS NULL))
);

CREATE TABLE import_item_dos_entries (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  normalized_path TEXT NOT NULL,
  original_relative_path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('EXE','COM','BAT')),
  rank INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  direct_launch_safe INTEGER NOT NULL CHECK(direct_launch_safe IN (0,1)),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_item_id, normalized_path)
);

CREATE TABLE import_item_core_validations (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  platform_instance_version INTEGER NOT NULL,
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  dat_version_id TEXT REFERENCES dat_versions(id),
  default_dos_entry TEXT,
  source_manifest_digest TEXT NOT NULL,
  prepublish_input_digest TEXT NOT NULL CHECK(length(prepublish_input_digest) = 64),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(import_item_id, prepublish_input_digest)
);

CREATE TABLE import_item_validation_files (
  import_item_core_validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL,
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_item_core_validation_id, role, logical_name)
);

CREATE INDEX import_items_queue ON import_items(state, updated_at_ms, id);

CREATE TRIGGER import_item_source_files_immutable_update BEFORE UPDATE ON import_item_source_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_source_files_immutable_delete BEFORE DELETE ON import_item_source_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_core_validations_immutable_update BEFORE UPDATE ON import_item_core_validations BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_core_validations_immutable_delete BEFORE DELETE ON import_item_core_validations BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_validation_files_immutable_update BEFORE UPDATE ON import_item_validation_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER import_item_validation_files_immutable_delete BEFORE DELETE ON import_item_validation_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
