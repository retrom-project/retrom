-- retrom: rebuild-with-foreign-keys-off
-- Widen the closed job kind catalog while retaining every schema-025 row.

DROP TRIGGER review_multidisc_attachment_owner_insert;

CREATE TABLE jobs_v26 (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_REVALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_FILE_REVISION','BLOB_GC','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE','SERVER_BIOS_IMPORT'
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
  CHECK(kind<>'SERVER_BIOS_IMPORT' OR scope_type='SERVER_IMPORT')
);
INSERT INTO jobs_v26 SELECT * FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_v26 RENAME TO jobs;
CREATE INDEX jobs_claim ON jobs(state,available_at_ms);
CREATE INDEX jobs_scope ON jobs(scope_type,scope_id);

CREATE TABLE job_events_v26 (
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
INSERT INTO job_events_v26 SELECT * FROM job_events;
DROP TABLE job_events;
ALTER TABLE job_events_v26 RENAME TO job_events;
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

CREATE TABLE server_imports (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(kind='BIOS_DIRECTORY'),
  root_id TEXT NOT NULL CHECK(length(CAST(root_id AS BLOB)) BETWEEN 1 AND 32),
  root_label_snapshot TEXT NOT NULL CHECK(length(root_label_snapshot) BETWEEN 1 AND 40 AND length(CAST(root_label_snapshot AS BLOB))<=160),
  source_relative_path TEXT NOT NULL CHECK(length(CAST(source_relative_path AS BLOB))<=4096),
  root_config_digest TEXT NOT NULL CHECK(length(root_config_digest)=64 AND root_config_digest=lower(root_config_digest)),
  catalog_snapshot_digest TEXT NOT NULL CHECK(length(catalog_snapshot_digest)=64 AND catalog_snapshot_digest=lower(catalog_snapshot_digest)),
  replace_if_better INTEGER NOT NULL CHECK(replace_if_better IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','COMPLETED','PARTIAL_FAILURE','CANCEL_REQUESTED','CANCELLED','FAILED')),
  phase TEXT CHECK(phase IS NULL OR phase IN ('PREPARING_ROOT','DISCOVERING','HASHING','VALIDATING_ARCHIVES','DISCOVERY_COMPLETED','RANKING','INSTALLING','QUEUEING_REVALIDATION')),
  catalog_item_count INTEGER NOT NULL CHECK(catalog_item_count>=0),
  candidate_count INTEGER NOT NULL DEFAULT 0 CHECK(candidate_count>=0),
  evaluated_item_count INTEGER NOT NULL DEFAULT 0 CHECK(evaluated_item_count>=0),
  multi_candidate_item_count INTEGER NOT NULL DEFAULT 0 CHECK(multi_candidate_item_count>=0),
  imported_matched_count INTEGER NOT NULL DEFAULT 0 CHECK(imported_matched_count>=0),
  imported_warning_count INTEGER NOT NULL DEFAULT 0 CHECK(imported_warning_count>=0),
  imported_missing_entry_count INTEGER NOT NULL DEFAULT 0 CHECK(imported_missing_entry_count>=0),
  not_found_count INTEGER NOT NULL DEFAULT 0 CHECK(not_found_count>=0),
  skipped_existing_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_existing_count>=0),
  skipped_not_better_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_not_better_count>=0),
  same_bytes_count INTEGER NOT NULL DEFAULT 0 CHECK(same_bytes_count>=0),
  failed_item_count INTEGER NOT NULL DEFAULT 0 CHECK(failed_item_count>=0),
  cancelled_item_count INTEGER NOT NULL DEFAULT 0 CHECK(cancelled_item_count>=0),
  skipped_special_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_special_count>=0),
  skipped_unrepresentable_path_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_unrepresentable_path_count>=0),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  last_error_code TEXT,
  cancel_requested_at_ms INTEGER,
  cancel_reason TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  CHECK((state IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED'))=(completed_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(imported_matched_count+imported_warning_count+imported_missing_entry_count+not_found_count+
        skipped_existing_count+skipped_not_better_count+same_bytes_count+failed_item_count+cancelled_item_count<=catalog_item_count),
  CHECK(state NOT IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED') OR
        imported_matched_count+imported_warning_count+imported_missing_entry_count+not_found_count+
        skipped_existing_count+skipped_not_better_count+same_bytes_count+failed_item_count+cancelled_item_count=catalog_item_count)
);
CREATE UNIQUE INDEX server_imports_one_active_kind ON server_imports(kind)
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');
CREATE INDEX server_imports_history ON server_imports(kind,created_at_ms DESC,id DESC);
CREATE INDEX server_imports_state ON server_imports(state,updated_at_ms DESC,id DESC);

CREATE TABLE server_bios_import_items (
  server_import_id TEXT NOT NULL REFERENCES server_imports(id),
  requirement_id TEXT NOT NULL REFERENCES bios_requirements(id),
  requirement_version INTEGER NOT NULL CHECK(requirement_version>=1),
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_name_snapshot TEXT NOT NULL,
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  core_artifact_version INTEGER NOT NULL CHECK(core_artifact_version>=1),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('STATIC','DAT_MACHINE')),
  logical_name TEXT NOT NULL,
  requirement_mode TEXT NOT NULL CHECK(requirement_mode IN ('REQUIRED','OPTIONAL','CONDITIONAL')),
  condition_code TEXT,
  activation_options_json TEXT,
  delivery_kind TEXT NOT NULL CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE')),
  emulator_path TEXT,
  source_version TEXT NOT NULL,
  catalog_digest TEXT NOT NULL CHECK(length(catalog_digest)=64 AND catalog_digest=lower(catalog_digest)),
  dat_version_id TEXT REFERENCES dat_versions(id),
  dat_machine_name TEXT,
  expected_size_bytes INTEGER CHECK(expected_size_bytes IS NULL OR expected_size_bytes>=0),
  expected_md5 TEXT,
  expected_sha1 TEXT,
  expected_sha256 TEXT,
  active_installation_id_snapshot TEXT REFERENCES bios_installations(id),
  active_installation_version_snapshot INTEGER,
  active_blob_sha256_snapshot TEXT,
  active_status_snapshot TEXT,
  active_validated_requirement_version_snapshot INTEGER,
  state TEXT NOT NULL CHECK(state IN ('PENDING','EVALUATING','IMPORTED_MATCHED','IMPORTED_WARNING','IMPORTED_MISSING_ENTRY','NOT_FOUND','SKIPPED_EXISTING','SKIPPED_NOT_BETTER','ALREADY_SAME_BYTES','SOURCE_CHANGED','CATALOG_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  candidate_count INTEGER NOT NULL DEFAULT 0 CHECK(candidate_count>=0),
  match_method TEXT CHECK(match_method IS NULL OR match_method IN ('EXACT_HASH','EXPECTED_SIZE_FALLBACK','LARGEST_SIZE_FALLBACK','DAT_ENTRY_MATCH','DAT_ENTRY_WARNING','DAT_PARTIAL_FALLBACK')),
  selection_details_json TEXT,
  previous_installation_id TEXT REFERENCES bios_installations(id),
  new_installation_id TEXT REFERENCES bios_installations(id),
  outcome_code TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  PRIMARY KEY(server_import_id,requirement_id),
  CHECK((source_kind='STATIC' AND dat_version_id IS NULL AND dat_machine_name IS NULL) OR
        (source_kind='DAT_MACHINE' AND dat_version_id IS NOT NULL AND dat_machine_name IS NOT NULL)),
  CHECK((state IN ('PENDING','EVALUATING'))=(completed_at_ms IS NULL))
);
CREATE INDEX server_bios_items_page ON server_bios_import_items(server_import_id,core_name_snapshot,logical_name,requirement_id);

CREATE TABLE server_bios_import_candidates (
  id TEXT PRIMARY KEY,
  server_import_id TEXT NOT NULL,
  requirement_id TEXT NOT NULL,
  relative_path TEXT NOT NULL CHECK(length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 4096),
  basename TEXT NOT NULL CHECK(length(CAST(basename AS BLOB)) BETWEEN 1 AND 255),
  association_kind TEXT NOT NULL CHECK(association_kind IN ('EXACT_NAME','CASEFOLD_NAME','RENAMED_HASH_MATCH')),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  crc32 TEXT,
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','EVALUATING','ELIGIBLE','INELIGIBLE','SELECTED','SOURCE_CHANGED','READ_FAILED','ARCHIVE_UNSAFE','DUPLICATE_BYTES')),
  exact_hash INTEGER CHECK(exact_hash IS NULL OR exact_hash IN (0,1)),
  expected_size_match INTEGER CHECK(expected_size_match IS NULL OR expected_size_match IN (0,1)),
  exact_basename INTEGER NOT NULL CHECK(exact_basename IN (0,1)),
  safe_archive INTEGER CHECK(safe_archive IS NULL OR safe_archive IN (0,1)),
  launchable INTEGER CHECK(launchable IS NULL OR launchable IN (0,1)),
  matched_count INTEGER CHECK(matched_count IS NULL OR matched_count>=0),
  aliased_count INTEGER CHECK(aliased_count IS NULL OR aliased_count>=0),
  mismatched_count INTEGER CHECK(mismatched_count IS NULL OR mismatched_count>=0),
  missing_count INTEGER CHECK(missing_count IS NULL OR missing_count>=0),
  extra_count INTEGER CHECK(extra_count IS NULL OR extra_count>=0),
  rank_ordinal INTEGER CHECK(rank_ordinal IS NULL OR rank_ordinal>=1),
  not_selected_reason TEXT,
  evaluation_details_json TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  evaluated_at_ms INTEGER,
  UNIQUE(server_import_id,requirement_id,relative_path),
  UNIQUE(server_import_id,requirement_id,rank_ordinal),
  FOREIGN KEY(server_import_id,requirement_id) REFERENCES server_bios_import_items(server_import_id,requirement_id)
);
CREATE UNIQUE INDEX server_bios_candidates_selected
ON server_bios_import_candidates(server_import_id,requirement_id) WHERE state='SELECTED';
CREATE INDEX server_bios_candidates_page
ON server_bios_import_candidates(server_import_id,requirement_id,rank_ordinal,id);

ALTER TABLE bios_installations ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'BROWSER_UPLOAD'
CHECK(source_kind IN ('BROWSER_UPLOAD','SERVER_DIRECTORY'));
ALTER TABLE bios_installations ADD COLUMN server_import_candidate_id TEXT REFERENCES server_bios_import_candidates(id);
CREATE UNIQUE INDEX bios_installations_server_candidate ON bios_installations(server_import_candidate_id)
WHERE server_import_candidate_id IS NOT NULL;

CREATE TRIGGER bios_installations_source_insert
BEFORE INSERT ON bios_installations
WHEN (NEW.source_kind='BROWSER_UPLOAD' AND NEW.server_import_candidate_id IS NOT NULL)
  OR (NEW.source_kind='SERVER_DIRECTORY' AND (
    NEW.server_import_candidate_id IS NULL OR NOT EXISTS(
      SELECT 1 FROM server_bios_import_candidates candidate
      WHERE candidate.id=NEW.server_import_candidate_id
      AND candidate.requirement_id=NEW.requirement_id AND candidate.state='SELECTED'
    )
  ))
BEGIN SELECT RAISE(ABORT,'invalid BIOS installation source'); END;
CREATE TRIGGER bios_installations_source_update
BEFORE UPDATE OF source_kind,server_import_candidate_id,requirement_id ON bios_installations
WHEN NEW.source_kind<>OLD.source_kind OR NEW.server_import_candidate_id IS NOT OLD.server_import_candidate_id OR NEW.requirement_id<>OLD.requirement_id
BEGIN SELECT RAISE(ABORT,'immutable BIOS installation source'); END;

CREATE TRIGGER server_import_job_insert
BEFORE INSERT ON server_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.job_id AND job.kind='SERVER_BIOS_IMPORT'
  AND job.scope_type='SERVER_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid server import job'); END;

CREATE TRIGGER server_bios_items_frozen_update
BEFORE UPDATE ON server_bios_import_items
WHEN NEW.server_import_id<>OLD.server_import_id OR NEW.requirement_id<>OLD.requirement_id OR
  NEW.requirement_version<>OLD.requirement_version OR NEW.core_id<>OLD.core_id OR
  NEW.core_name_snapshot<>OLD.core_name_snapshot OR NEW.core_artifact_id<>OLD.core_artifact_id OR
  NEW.core_artifact_version<>OLD.core_artifact_version OR NEW.source_kind<>OLD.source_kind OR
  NEW.logical_name<>OLD.logical_name OR NEW.requirement_mode<>OLD.requirement_mode OR
  NEW.condition_code IS NOT OLD.condition_code OR NEW.activation_options_json IS NOT OLD.activation_options_json OR
  NEW.delivery_kind<>OLD.delivery_kind OR NEW.emulator_path IS NOT OLD.emulator_path OR
  NEW.source_version<>OLD.source_version OR NEW.catalog_digest<>OLD.catalog_digest OR
  NEW.dat_version_id IS NOT OLD.dat_version_id OR NEW.dat_machine_name IS NOT OLD.dat_machine_name OR
  NEW.expected_size_bytes IS NOT OLD.expected_size_bytes OR NEW.expected_md5 IS NOT OLD.expected_md5 OR
  NEW.expected_sha1 IS NOT OLD.expected_sha1 OR NEW.expected_sha256 IS NOT OLD.expected_sha256 OR
  NEW.active_installation_id_snapshot IS NOT OLD.active_installation_id_snapshot OR
  NEW.active_installation_version_snapshot IS NOT OLD.active_installation_version_snapshot OR
  NEW.active_blob_sha256_snapshot IS NOT OLD.active_blob_sha256_snapshot OR
  NEW.active_status_snapshot IS NOT OLD.active_status_snapshot OR
  NEW.active_validated_requirement_version_snapshot IS NOT OLD.active_validated_requirement_version_snapshot OR
  NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable server BIOS item snapshot'); END;

CREATE TRIGGER server_bios_items_installation_update
BEFORE UPDATE OF previous_installation_id,new_installation_id ON server_bios_import_items
WHEN (NEW.previous_installation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bios_installations installation
  WHERE installation.id=NEW.previous_installation_id AND installation.requirement_id=NEW.requirement_id
)) OR (NEW.new_installation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bios_installations installation
  WHERE installation.id=NEW.new_installation_id AND installation.requirement_id=NEW.requirement_id
))
BEGIN SELECT RAISE(ABORT,'server BIOS item installation owner mismatch'); END;
