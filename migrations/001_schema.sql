-- PostgreSQL schema. Fresh databases only; no SQLite import or compatibility lineage.

CREATE TABLE profiles (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0)
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL UNIQUE ,
  username TEXT NOT NULL UNIQUE CHECK(
    length(username) BETWEEN 3 AND 32 AND
    username = lower(username) AND
    username ~ '^[a-z].*$' AND
    username !~ '^.*[^a-z0-9._-].*$' AND
    username NOT IN ('local','root','system','retrom')
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 320),
  role TEXT NOT NULL CHECK(role IN ('ADMIN','USER')),
  status TEXT NOT NULL CHECK(status IN ('ENABLED','DISABLED','DELETED')),
  session_version BIGINT NOT NULL DEFAULT 1 CHECK(session_version >= 1),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  last_login_at_ms BIGINT CHECK(last_login_at_ms IS NULL OR last_login_at_ms >= 0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms),
  disabled_at_ms BIGINT CHECK(disabled_at_ms IS NULL OR disabled_at_ms >= created_at_ms),
  deleted_at_ms BIGINT CHECK(deleted_at_ms IS NULL OR deleted_at_ms >= created_at_ms),
  CHECK(
    status = 'ENABLED' AND disabled_at_ms IS NULL AND deleted_at_ms IS NULL OR
    status = 'DISABLED' AND disabled_at_ms IS NOT NULL AND deleted_at_ms IS NULL OR
    status = 'DELETED' AND deleted_at_ms IS NOT NULL
  )
);

CREATE TABLE user_credentials (
  user_id TEXT PRIMARY KEY ,
  password_hash TEXT NOT NULL CHECK(length(password_hash) BETWEEN 1 AND 512),
  password_scheme TEXT NOT NULL CHECK(password_scheme='ARGON2ID_V1'),
  password_changed_at_ms BIGINT NOT NULL CHECK(password_changed_at_ms >= 0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  CHECK(password_changed_at_ms >= created_at_ms)
);

CREATE TABLE auth_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL ,
  token_sha256 BYTEA NOT NULL UNIQUE CHECK(length(token_sha256)=32),
  user_session_version BIGINT NOT NULL CHECK(user_session_version >= 1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  last_seen_at_ms BIGINT NOT NULL CHECK(last_seen_at_ms >= created_at_ms),
  idle_expires_at_ms BIGINT NOT NULL CHECK(idle_expires_at_ms >= last_seen_at_ms),
  absolute_expires_at_ms BIGINT NOT NULL CHECK(absolute_expires_at_ms >= idle_expires_at_ms),
  revoked_at_ms BIGINT,
  revoked_reason TEXT CHECK(revoked_reason IN (
    'LOGOUT','PASSWORD_CHANGED','PASSWORD_RESET','ROLE_CHANGED','USER_DISABLED',
    'USER_DELETED','EXPIRED'
  )),
  CHECK((revoked_at_ms IS NULL) = (revoked_reason IS NULL)),
  CHECK(revoked_at_ms IS NULL OR revoked_at_ms >= created_at_ms)
);

CREATE TABLE account_links (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(kind IN ('INVITATION','PASSWORD_RESET')),
  invited_role TEXT CHECK(invited_role IN ('ADMIN','USER')),
  target_user_id TEXT ,
  created_by_user_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  expires_at_ms BIGINT NOT NULL CHECK(expires_at_ms = created_at_ms + 3600000),
  consumed_at_ms BIGINT,
  consumed_by_user_id TEXT ,
  revoked_at_ms BIGINT,
  revoked_by_kind TEXT CHECK(revoked_by_kind IN ('USER','SYSTEM')),
  revoked_by_user_id TEXT ,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  CHECK(
    kind='INVITATION' AND invited_role IS NOT NULL AND target_user_id IS NULL OR
    kind='PASSWORD_RESET' AND invited_role IS NULL AND target_user_id IS NOT NULL
  ),
  CHECK((consumed_at_ms IS NULL) = (consumed_by_user_id IS NULL)),
  CHECK(
    revoked_at_ms IS NULL AND revoked_by_kind IS NULL AND revoked_by_user_id IS NULL OR
    revoked_at_ms IS NOT NULL AND revoked_by_kind='SYSTEM' AND revoked_by_user_id IS NULL OR
    revoked_at_ms IS NOT NULL AND revoked_by_kind='USER' AND revoked_by_user_id IS NOT NULL
  ),
  CHECK(consumed_at_ms IS NULL OR revoked_at_ms IS NULL)
);

CREATE TABLE instance_state (
  id BIGINT PRIMARY KEY CHECK(id=1),
  state TEXT NOT NULL CHECK(state IN ('PENDING','COMPLETED')),
  bootstrap_kind TEXT CHECK(bootstrap_kind IN ('RELEASE_SETUP','TEST_DEFAULT')),
  initial_admin_user_id TEXT ,
  test_default_password_active BIGINT NOT NULL DEFAULT 0 CHECK(test_default_password_active IN (0,1)),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms),
  initialized_at_ms BIGINT,
  CHECK(
    state='PENDING' AND bootstrap_kind IS NULL AND initial_admin_user_id IS NULL AND
      test_default_password_active=0 AND initialized_at_ms IS NULL OR
    state='COMPLETED' AND bootstrap_kind IS NOT NULL AND initial_admin_user_id IS NOT NULL AND
      initialized_at_ms IS NOT NULL AND initialized_at_ms >= created_at_ms
  ),
  CHECK(test_default_password_active=0 OR bootstrap_kind='TEST_DEFAULT')
);

CREATE TABLE auth_rate_limits (
  scope TEXT NOT NULL CHECK(scope IN ('LOGIN_ACCOUNT','LOGIN_IP','SETUP_IP','LINK_IP')),
  subject_hash BYTEA NOT NULL CHECK(length(subject_hash)=32),
  window_started_at_ms BIGINT NOT NULL CHECK(window_started_at_ms >= 0),
  failure_count BIGINT NOT NULL CHECK(failure_count >= 0),
  blocked_until_ms BIGINT CHECK(blocked_until_ms IS NULL OR blocked_until_ms >= window_started_at_ms),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= window_started_at_ms),
  PRIMARY KEY(scope,subject_hash)
);

CREATE TABLE platforms (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  sort_order BIGINT NOT NULL,
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE cores (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE content_kinds (
  id TEXT PRIMARY KEY CHECK(
    length(id) BETWEEN 2 AND 64 AND id=upper(id) AND id !~ '^.*[^A-Z0-9_].*$'
  )
);

CREATE TABLE platform_cores (
  platform_id TEXT NOT NULL ,
  core_id TEXT NOT NULL ,
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  PRIMARY KEY(platform_id, core_id)
);

CREATE TABLE runtime_providers (
  provider_id TEXT PRIMARY KEY CHECK(
    length(provider_id) BETWEEN 1 AND 64 AND provider_id=lower(provider_id)
    AND provider_id !~ '^.*[^a-z0-9-].*$'
  ),
  provider_version TEXT NOT NULL CHECK(length(provider_version) BETWEEN 5 AND 128),
  provider_api_version BIGINT NOT NULL CHECK(provider_api_version>=1),
  bundle_sha256 TEXT NOT NULL UNIQUE CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256=lower(manifest_sha256)),
  module_sha256 TEXT NOT NULL CHECK(length(module_sha256)=64 AND module_sha256=lower(module_sha256)),
  source TEXT NOT NULL CHECK(source IN ('candidate','production')),
  release_repository TEXT,
  release_tag TEXT,
  release_commit TEXT,
  activated_at_ms BIGINT NOT NULL CHECK(activated_at_ms>=0),
  UNIQUE(provider_id,bundle_sha256),
  CHECK(
    source='candidate' AND release_repository IS NULL AND release_tag IS NULL AND release_commit IS NULL
    OR source='production' AND release_repository IS NOT NULL AND release_tag IS NOT NULL
      AND length(release_commit)=40 AND release_commit=lower(release_commit)
  )
);

CREATE TABLE runtime_catalog_state (
  singleton BIGINT PRIMARY KEY CHECK(singleton=1),
  catalog_sha256 TEXT NOT NULL CHECK(length(catalog_sha256)=64 AND catalog_sha256=lower(catalog_sha256)),
  activated_at_ms BIGINT NOT NULL CHECK(activated_at_ms>=0)
);

CREATE TABLE runtime_target_bindings (
  binding_id TEXT PRIMARY KEY CHECK(
    length(binding_id) BETWEEN 1 AND 128 AND binding_id=lower(binding_id)
    AND binding_id !~ '^.*[^a-z0-9-].*$'
  ),
  core_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  detector_profile TEXT NOT NULL CHECK(length(detector_profile) BETWEEN 2 AND 64),
  delivery_profile TEXT NOT NULL CHECK(length(delivery_profile) BETWEEN 2 AND 64),
  launch_policy TEXT NOT NULL CHECK(
    length(launch_policy) BETWEEN 2 AND 64 AND launch_policy=upper(launch_policy)
    AND launch_policy !~ '^.*[^A-Z0-9_].*$'
  ),
  UNIQUE(provider_id,target_id)
);

CREATE TABLE runtime_binding_platforms (
  binding_id TEXT NOT NULL ,
  platform_id TEXT NOT NULL,
  core_id TEXT NOT NULL,
  PRIMARY KEY(binding_id,platform_id)
);

CREATE TABLE runtime_binding_content_kinds (
  binding_id TEXT NOT NULL ,
  content_kind TEXT NOT NULL ,
  PRIMARY KEY(binding_id,content_kind)
);

CREATE TABLE platform_instances (
  id TEXT PRIMARY KEY,
  platform_id TEXT NOT NULL,
  default_core_id TEXT NOT NULL,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
  slug TEXT NOT NULL CHECK(slug = lower(slug) AND slug NOT LIKE '-%' AND slug NOT LIKE '%-' AND slug NOT LIKE '%--%'),
  description TEXT NOT NULL DEFAULT '',
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms),
  deleted_at_ms BIGINT CHECK(deleted_at_ms IS NULL OR deleted_at_ms >= created_at_ms),
  catalog_template_key TEXT
CHECK (
  catalog_template_key IS NULL OR (
    length(catalog_template_key) BETWEEN 3 AND 160
    AND catalog_template_key = lower(catalog_template_key)
    AND catalog_template_key !~ '^.*[^a-z0-9_/-].*$'
    AND catalog_template_key ~ '^.*/.*$'
    AND catalog_template_key !~ '^.*/.*/.*$'
  )
),
  UNIQUE(platform_id, slug),
  UNIQUE(id, platform_id)
);

CREATE TABLE "runtime_targets" (
  provider_id TEXT NOT NULL ,
  target_id TEXT NOT NULL CHECK(
    length(target_id) BETWEEN 1 AND 64 AND target_id=lower(target_id)
    AND target_id !~ '^.*[^a-z0-9-].*$'
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 120),
  target_options_schema_json TEXT NOT NULL CHECK(
    (target_options_schema_json IS JSON) AND jsonb_typeof((target_options_schema_json)::jsonb)='object'
  ),
  capabilities_json TEXT NOT NULL CHECK((capabilities_json IS JSON)),
  checkpoint_json TEXT CHECK(checkpoint_json IS NULL OR (checkpoint_json IS JSON)),
  manifest_fragment_json TEXT NOT NULL CHECK((manifest_fragment_json IS JSON)),
  PRIMARY KEY(provider_id,target_id)
);

CREATE TABLE "job_events" (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  job_id TEXT NOT NULL ,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  event_type TEXT NOT NULL CHECK(event_type IN (
    'QUEUED','STARTED','PROGRESS','RETRY_SCHEDULED','CANCEL_REQUESTED','MANUAL_RETRY',
    'ARCHIVE_SCANNED','PARENT_MATCHED','PARENT_REJECTED','SOURCE_SNAPSHOT_CREATED',
    'CORE_VALIDATION_COMPLETED','PLAYLIST_PARSED','DISC_SET_MATCHED','DISC_SET_REJECTED',
    'SUCCEEDED','FAILED','CANCELLED'
  )),
  data_json TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0)
);

CREATE TABLE job_input_snapshots (
  job_id TEXT NOT NULL ,
  execution_no BIGINT NOT NULL CHECK(execution_no >= 1),
  input_json TEXT NOT NULL,
  input_digest TEXT NOT NULL CHECK(length(input_digest) = 64),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  PRIMARY KEY(job_id, execution_no)
);

CREATE TABLE "idempotency_records" (
  principal_id TEXT NOT NULL CHECK(length(principal_id) BETWEEN 1 AND 128),
  operation_id TEXT NOT NULL,
  key TEXT NOT NULL,
  request_digest TEXT NOT NULL CHECK(length(request_digest) = 64),
  http_status BIGINT NOT NULL CHECK(http_status BETWEEN 100 AND 599),
  response_headers_json TEXT NOT NULL,
  response_body BYTEA NOT NULL CHECK(length(response_body) <= 1048576),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  expires_at_ms BIGINT NOT NULL CHECK(expires_at_ms >= created_at_ms),
  PRIMARY KEY(principal_id, operation_id, key)
);

CREATE TABLE "audit_events" (
  id TEXT PRIMARY KEY,
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT ,
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','startup-test-bootstrap',
    'payload-release-worker','runtime-provider-reconciliation'
  )),
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  before_json TEXT,
  after_json TEXT,
  diff_json TEXT,
  request_id TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);

CREATE TABLE "jobs" (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_VALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_CONTENT_REPLACE','PATH_DELETE','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE','SERVER_BIOS_IMPORT',
    'IMPORT_SCAN','IMPORT_RECEIVE',
    'REVIEW_BULK_APPROVE','OWNER_CLEANUP'
  )),
  dedupe_key TEXT NOT NULL CHECK(length(dedupe_key)=64),
  execution_no BIGINT NOT NULL CHECK(execution_no>=1),
  payload_json TEXT NOT NULL,
  cancellable BIGINT NOT NULL CHECK(cancellable IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','CANCEL_REQUESTED','SUCCEEDED','FAILED','CANCELLED')),
  attempt_count BIGINT NOT NULL CHECK(attempt_count>=0),
  max_attempts BIGINT NOT NULL CHECK(max_attempts BETWEEN 1 AND 4),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  available_at_ms BIGINT NOT NULL CHECK(available_at_ms>=0),
  execution_started_at_ms BIGINT,
  execution_deadline_at_ms BIGINT,
  leased_until_ms BIGINT,
  heartbeat_at_ms BIGINT,
  finished_at_ms BIGINT,
  worker_id TEXT,
  error_code TEXT,
  error_retryable BIGINT CHECK(error_retryable IS NULL OR error_retryable IN (0,1)),
  cancel_requested_at_ms BIGINT,
  cancel_reason TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(kind,dedupe_key),
  CHECK((state IN ('SUCCEEDED','FAILED','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(kind NOT IN ('REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE') OR scope_type='IMPORT_ITEM'),
  CHECK(kind<>'SERVER_BIOS_IMPORT' OR scope_type='SERVER_IMPORT'),
  CHECK(kind NOT IN ('IMPORT_SCAN','IMPORT_RECEIVE') OR scope_type='SOURCE_IMPORT'),
  CHECK(kind<>'REVIEW_BULK_APPROVE' OR scope_type='REVIEW_BULK_APPROVAL'),
  CHECK(kind<>'OWNER_CLEANUP' OR scope_type IN (
    'IMPORT_ITEM','IMPORT_JOB','SOURCE_IMPORT_ITEM','UPLOAD_CONSUMPTION','GAME'
  )),
  CHECK(kind<>'OWNER_CLEANUP' OR (cancellable=0 AND max_attempts=4)),
  CHECK(kind<>'PATH_DELETE' OR (scope_type='STORAGE_PATH' AND cancellable=0 AND max_attempts=4))
);

CREATE TABLE upload_sessions (
  id TEXT PRIMARY KEY,
  purpose TEXT NOT NULL DEFAULT 'GENERAL' CHECK(purpose IN ('GENERAL','PROJECT')),
  state TEXT NOT NULL CHECK(state IN ('CREATED','UPLOADING','FINALIZING','COMPLETE','FAILED','CANCELLED','EXPIRED')),
  source_type TEXT NOT NULL CHECK(source_type IN ('FILES','DIRECTORY')),
  total_files BIGINT NOT NULL CHECK(total_files BETWEEN 1 AND 10000),
  total_bytes BIGINT NOT NULL CHECK(total_bytes BETWEEN 0 AND 34359738368),
  manifest_digest TEXT NOT NULL CHECK(length(manifest_digest) = 64),
  finalization_no BIGINT NOT NULL DEFAULT 0 CHECK(finalization_no >= 0),
  finalize_job_id TEXT UNIQUE ,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  expires_at_ms BIGINT NOT NULL,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms),
  unconsumed_pruned_at_ms BIGINT,
  last_error_code TEXT,
  CHECK(purpose='GENERAL' OR source_type='DIRECTORY' OR total_files=1)
);

CREATE TABLE upload_files (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL ,
  relative_path TEXT NOT NULL,
  declared_size_bytes BIGINT NOT NULL CHECK(declared_size_bytes BETWEEN 0 AND 8589934592),
  received_size_bytes BIGINT NOT NULL DEFAULT 0 CHECK(received_size_bytes >= 0),
  final_file_record TEXT,
  state TEXT NOT NULL CHECK(state IN ('PENDING','PARTIAL','FINALIZING','COMPLETE','FAILED','PURGED')),
  payload_released_at_ms BIGINT,
  last_error_code TEXT,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  UNIQUE(upload_session_id, relative_path),
  CHECK(
    state='COMPLETE' AND final_file_record IS NOT NULL AND payload_released_at_ms IS NULL OR
    state='PURGED' AND final_file_record IS NULL AND payload_released_at_ms IS NOT NULL OR
    state NOT IN ('COMPLETE','PURGED') AND final_file_record IS NULL AND payload_released_at_ms IS NULL
  )
);

CREATE TABLE import_files (
  id TEXT PRIMARY KEY ,
  upload_session_id TEXT NOT NULL ,
  relative_path TEXT NOT NULL,
  file_record TEXT,
  size_bytes BIGINT NOT NULL CHECK(size_bytes >= 0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  released_at_ms BIGINT,
  UNIQUE(upload_session_id, relative_path),
  CHECK((file_record IS NULL) = (released_at_ms IS NOT NULL))
);

CREATE TABLE upload_parts (
  upload_file_id TEXT NOT NULL ,
  part_no BIGINT NOT NULL CHECK(part_no >= 0),
  offset_bytes BIGINT NOT NULL CHECK(offset_bytes >= 0),
  size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 1 AND 8388608),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  storage_key TEXT NOT NULL UNIQUE,
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(upload_file_id, part_no)
);

CREATE TABLE archive_entries (
  archive_file_record TEXT NOT NULL,
  ordinal BIGINT NOT NULL CHECK(ordinal >= 0),
  original_relative_path TEXT NOT NULL,
  normalized_path TEXT NOT NULL,
  ascii_casefold_path TEXT NOT NULL,
  archive_format TEXT NOT NULL CHECK(archive_format IN ('ZIP','SEVEN_Z','ELECTRON_ASAR')),
  compression_profile TEXT NOT NULL CHECK(compression_profile IN (
    'STORE','DEFLATE','SEVEN_Z_DECODER_VALIDATED','ELECTRON_ASAR_STORE','ELECTRON_ASAR_DEFLATE'
  )),
  uncompressed_size_bytes BIGINT NOT NULL CHECK(uncompressed_size_bytes >= 0),
  crc32 TEXT NOT NULL CHECK(length(crc32) = 8),
  md5 TEXT NOT NULL CHECK(length(md5) = 32),
  sha1 TEXT NOT NULL CHECK(length(sha1) = 40),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(archive_file_record, ordinal),
  UNIQUE(archive_file_record, normalized_path),
  UNIQUE(archive_file_record, ascii_casefold_path),
  CHECK((archive_format='ZIP' AND compression_profile IN ('STORE','DEFLATE')) OR
        (archive_format='SEVEN_Z' AND compression_profile='SEVEN_Z_DECODER_VALIDATED') OR
        (archive_format='ELECTRON_ASAR' AND compression_profile IN ('ELECTRON_ASAR_STORE','ELECTRON_ASAR_DEFLATE')))
);

CREATE TABLE "upload_consumptions" (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL ,
  upload_file_id TEXT ,
  consumer_type TEXT NOT NULL CHECK(consumer_type IN (
    'IMPORT_JOB','GAME_CONTENT_REPLACE_JOB','GAME_ASSET','REVIEW_ASSET','REVIEW_ARCADE_PARENT',
    'REVIEW_MULTI_DISC','BIOS_INSTALLATION'
  )),
  consumer_id TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  released_at_ms BIGINT,
  release_reason TEXT CHECK(release_reason IS NULL OR release_reason IN (
    'IMPORT_PUBLISHED','IMPORT_DISCARDED','IMPORT_FAILED_FINAL','IMPORT_CANCELLED',
    'IMPORT_JOB_TERMINAL','SOURCE_TERMINAL','UPLOAD_CONSUMED','GAME_DELETED'
  )),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(consumer_type,consumer_id),
  CHECK((released_at_ms IS NULL)=(release_reason IS NULL))
);

CREATE TABLE bios_installations (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL ,
  file_record TEXT,
  original_filename TEXT NOT NULL,
  size_bytes BIGINT NOT NULL CHECK(size_bytes >= 0),
  md5 TEXT NOT NULL CHECK(length(md5) = 32),
  sha1 TEXT NOT NULL CHECK(length(sha1) = 40),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  validated_requirement_version BIGINT NOT NULL CHECK(validated_requirement_version >= 1),
  status TEXT NOT NULL CHECK(status IN ('MATCHED','HASH_WARNING','MISSING_ENTRY','INVALID')),
  validation_details_json TEXT NOT NULL,
  is_active BIGINT NOT NULL CHECK(is_active IN (0,1)),
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  source_kind TEXT NOT NULL DEFAULT 'BROWSER_UPLOAD'
CHECK(source_kind IN ('BROWSER_UPLOAD','SERVER_DIRECTORY')),
  server_import_candidate_id TEXT ,
  payload_released_at_ms BIGINT CHECK(payload_released_at_ms IS NULL OR payload_released_at_ms>=created_at_ms),
  CHECK(NOT (status = 'INVALID' AND is_active = 1)),
  CHECK(is_active=0 OR file_record IS NOT NULL),
  CHECK((file_record IS NULL)=(payload_released_at_ms IS NOT NULL))
);

CREATE TABLE dat_machines (
  dat_version_id TEXT NOT NULL ,
  machine_name TEXT NOT NULL,
  description TEXT NOT NULL,
  year TEXT NOT NULL,
  manufacturer TEXT NOT NULL,
  cloneof TEXT,
  romof TEXT,
  is_explicit_bios BIGINT NOT NULL CHECK(is_explicit_bios IN (0,1)),
  classification TEXT NOT NULL CHECK(classification IN ('NORMAL','EXPLICIT_BIOS','ROMOF_INFERENCE')),
  PRIMARY KEY(dat_version_id, machine_name)
);

CREATE TABLE dat_rom_entries (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  ordinal BIGINT NOT NULL CHECK(ordinal >= 0),
  name TEXT NOT NULL,
  size_bytes BIGINT NOT NULL CHECK(size_bytes >= 0),
  crc32 TEXT,
  sha1 TEXT,
  status TEXT CHECK(status IS NULL OR status IN ('GOOD','NODUMP','BADDUMP')),
  merge_name TEXT,
  bios_name TEXT,
  PRIMARY KEY(dat_version_id, machine_name, ordinal),
  CHECK(status = 'NODUMP' OR crc32 IS NOT NULL OR sha1 IS NOT NULL)
);

CREATE TABLE dat_disk_entries (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  ordinal BIGINT NOT NULL CHECK(ordinal >= 0),
  name TEXT NOT NULL,
  sha1 TEXT,
  status TEXT CHECK(status IS NULL OR status IN ('GOOD','NODUMP','BADDUMP')),
  PRIMARY KEY(dat_version_id, machine_name, ordinal),
  CHECK(status = 'NODUMP' OR sha1 IS NOT NULL)
);

CREATE TABLE dat_bios_sets (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  bios_name TEXT NOT NULL,
  description TEXT NOT NULL,
  is_default BIGINT NOT NULL CHECK(is_default IN (0,1)),
  PRIMARY KEY(dat_version_id, machine_name, bios_name)
);

CREATE TABLE server_imports (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(kind='BIOS_DIRECTORY'),
  root_id TEXT NOT NULL CHECK(octet_length(root_id) BETWEEN 1 AND 32),
  root_label_snapshot TEXT NOT NULL CHECK(length(root_label_snapshot) BETWEEN 1 AND 40 AND octet_length(root_label_snapshot)<=160),
  source_relative_path TEXT NOT NULL CHECK(octet_length(source_relative_path)<=4096),
  root_config_digest TEXT NOT NULL CHECK(length(root_config_digest)=64 AND root_config_digest=lower(root_config_digest)),
  catalog_snapshot_digest TEXT NOT NULL CHECK(length(catalog_snapshot_digest)=64 AND catalog_snapshot_digest=lower(catalog_snapshot_digest)),
  replace_if_better BIGINT NOT NULL CHECK(replace_if_better IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','COMPLETED','PARTIAL_FAILURE','CANCEL_REQUESTED','CANCELLED','FAILED')),
  phase TEXT CHECK(phase IS NULL OR phase IN ('PREPARING_ROOT','DISCOVERING','HASHING','VALIDATING_ARCHIVES','DISCOVERY_COMPLETED','RANKING','INSTALLING','QUEUEING_REVALIDATION')),
  catalog_item_count BIGINT NOT NULL CHECK(catalog_item_count>=0),
  candidate_count BIGINT NOT NULL DEFAULT 0 CHECK(candidate_count>=0),
  evaluated_item_count BIGINT NOT NULL DEFAULT 0 CHECK(evaluated_item_count>=0),
  multi_candidate_item_count BIGINT NOT NULL DEFAULT 0 CHECK(multi_candidate_item_count>=0),
  imported_matched_count BIGINT NOT NULL DEFAULT 0 CHECK(imported_matched_count>=0),
  imported_warning_count BIGINT NOT NULL DEFAULT 0 CHECK(imported_warning_count>=0),
  imported_missing_entry_count BIGINT NOT NULL DEFAULT 0 CHECK(imported_missing_entry_count>=0),
  not_found_count BIGINT NOT NULL DEFAULT 0 CHECK(not_found_count>=0),
  skipped_existing_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_existing_count>=0),
  skipped_not_better_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_not_better_count>=0),
  same_bytes_count BIGINT NOT NULL DEFAULT 0 CHECK(same_bytes_count>=0),
  failed_item_count BIGINT NOT NULL DEFAULT 0 CHECK(failed_item_count>=0),
  cancelled_item_count BIGINT NOT NULL DEFAULT 0 CHECK(cancelled_item_count>=0),
  skipped_special_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_special_count>=0),
  skipped_unrepresentable_path_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_unrepresentable_path_count>=0),
  job_id TEXT NOT NULL UNIQUE ,
  created_by_user_id TEXT NOT NULL ,
  last_error_code TEXT,
  cancel_requested_at_ms BIGINT,
  cancel_reason TEXT,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms BIGINT,
  CHECK((state IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED'))=(completed_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(imported_matched_count+imported_warning_count+imported_missing_entry_count+not_found_count+
        skipped_existing_count+skipped_not_better_count+same_bytes_count+failed_item_count+cancelled_item_count<=catalog_item_count),
  CHECK(state NOT IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED') OR
        imported_matched_count+imported_warning_count+imported_missing_entry_count+not_found_count+
        skipped_existing_count+skipped_not_better_count+same_bytes_count+failed_item_count+cancelled_item_count=catalog_item_count)
);

CREATE TABLE server_bios_import_candidates (
  storage_file_id TEXT,
  id TEXT PRIMARY KEY,
  server_import_id TEXT NOT NULL,
  requirement_id TEXT NOT NULL,
  relative_path TEXT NOT NULL CHECK(octet_length(relative_path) BETWEEN 1 AND 4096),
  basename TEXT NOT NULL CHECK(octet_length(basename) BETWEEN 1 AND 255),
  association_kind TEXT NOT NULL CHECK(association_kind IN ('EXACT_NAME','CASEFOLD_NAME','RENAMED_HASH_MATCH')),
  size_bytes BIGINT NOT NULL CHECK(size_bytes>=0),
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  crc32 TEXT,
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','EVALUATING','ELIGIBLE','INELIGIBLE','SELECTED','SOURCE_CHANGED','READ_FAILED','ARCHIVE_UNSAFE','INVALID_ARCHIVE','CATALOG_INVALID','VALIDATION_FAILED','DUPLICATE_BYTES')),
  exact_hash BIGINT CHECK(exact_hash IS NULL OR exact_hash IN (0,1)),
  expected_size_match BIGINT CHECK(expected_size_match IS NULL OR expected_size_match IN (0,1)),
  exact_basename BIGINT NOT NULL CHECK(exact_basename IN (0,1)),
  safe_archive BIGINT CHECK(safe_archive IS NULL OR safe_archive IN (0,1)),
  launchable BIGINT CHECK(launchable IS NULL OR launchable IN (0,1)),
  matched_count BIGINT CHECK(matched_count IS NULL OR matched_count>=0),
  aliased_count BIGINT CHECK(aliased_count IS NULL OR aliased_count>=0),
  mismatched_count BIGINT CHECK(mismatched_count IS NULL OR mismatched_count>=0),
  missing_count BIGINT CHECK(missing_count IS NULL OR missing_count>=0),
  extra_count BIGINT CHECK(extra_count IS NULL OR extra_count>=0),
  rank_ordinal BIGINT CHECK(rank_ordinal IS NULL OR rank_ordinal>=1),
  not_selected_reason TEXT,
  evaluation_details_json TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  evaluated_at_ms BIGINT,
  UNIQUE(server_import_id,requirement_id,relative_path),
  UNIQUE(server_import_id,requirement_id,rank_ordinal)
);

CREATE TABLE "bios_requirements" (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('STATIC','DAT_MACHINE')),
  archive_members_json TEXT CHECK(archive_members_json IS NULL OR ((archive_members_json IS JSON) AND jsonb_typeof((archive_members_json)::jsonb)='array' AND source_kind='STATIC')),
  file_kind TEXT GENERATED ALWAYS AS (CASE WHEN source_kind='DAT_MACHINE' OR archive_members_json IS NOT NULL THEN 'ARCHIVE' ELSE 'FILE' END) VIRTUAL,
  dat_machine_name TEXT,
  logical_name TEXT NOT NULL,
  requirement_mode TEXT NOT NULL CHECK(requirement_mode IN ('REQUIRED','OPTIONAL','CONDITIONAL')),
  condition_code TEXT,
  activation_options_json TEXT,
  catalog_digest TEXT NOT NULL CHECK(length(catalog_digest) = 64),
  size_bytes BIGINT CHECK(size_bytes IS NULL OR size_bytes >= 0),
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  source_url TEXT NOT NULL,
  source_version TEXT NOT NULL,
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  delivery_kind TEXT NOT NULL DEFAULT 'BIOS_BUNDLE'
CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE')),
  emulator_path TEXT,
  UNIQUE(provider_id,target_id,logical_name),
  CHECK((source_kind = 'STATIC' AND dat_machine_name IS NULL) OR (source_kind = 'DAT_MACHINE' AND dat_machine_name IS NOT NULL))
);

CREATE TABLE "dat_versions" (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  builtin_relative_path TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  parser_version TEXT NOT NULL,
  parse_status TEXT NOT NULL CHECK(parse_status IN ('PENDING','PARSING','READY','FAILED','CANCELLED')),
  is_active BIGINT NOT NULL CHECK(is_active IN (0,1)),
  machine_count BIGINT,
  rom_entry_count BIGINT,
  disk_entry_count BIGINT,
  bios_set_count BIGINT,
  default_bios_set_count BIGINT,
  explicit_bios_machine_count BIGINT,
  base_dependency_target_count BIGINT,
  unresolved_relation_count BIGINT,
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  parsed_at_ms BIGINT,
  activated_at_ms BIGINT,
  UNIQUE(id,provider_id,target_id),
  UNIQUE(provider_id,target_id,sha256,parser_version),
  CHECK((parse_status = 'READY') = (parsed_at_ms IS NOT NULL)),
  CHECK(is_active = 0 OR parse_status = 'READY')
);

CREATE TABLE "server_bios_import_items" (
  server_import_id TEXT NOT NULL ,
  requirement_id TEXT NOT NULL ,
  requirement_version BIGINT NOT NULL CHECK(requirement_version>=1),
  core_id TEXT NOT NULL ,
  core_name_snapshot TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('STATIC','DAT_MACHINE')),
  archive_members_json TEXT CHECK(archive_members_json IS NULL OR ((archive_members_json IS JSON) AND jsonb_typeof((archive_members_json)::jsonb)='array' AND source_kind='STATIC')),
  file_kind TEXT GENERATED ALWAYS AS (CASE WHEN source_kind='DAT_MACHINE' OR archive_members_json IS NOT NULL THEN 'ARCHIVE' ELSE 'FILE' END) VIRTUAL,
  logical_name TEXT NOT NULL,
  requirement_mode TEXT NOT NULL CHECK(requirement_mode IN ('REQUIRED','OPTIONAL','CONDITIONAL')),
  condition_code TEXT,
  activation_options_json TEXT,
  delivery_kind TEXT NOT NULL CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE')),
  emulator_path TEXT,
  source_version TEXT NOT NULL,
  catalog_digest TEXT NOT NULL CHECK(length(catalog_digest)=64 AND catalog_digest=lower(catalog_digest)),
  dat_version_id TEXT,
  dat_machine_name TEXT,
  expected_size_bytes BIGINT CHECK(expected_size_bytes IS NULL OR expected_size_bytes>=0),
  expected_md5 TEXT,
  expected_sha1 TEXT,
  expected_sha256 TEXT,
  active_installation_id_snapshot TEXT ,
  active_installation_version_snapshot BIGINT,
  active_blob_sha256_snapshot TEXT,
  active_status_snapshot TEXT,
  active_validated_requirement_version_snapshot BIGINT,
  state TEXT NOT NULL CHECK(state IN ('PENDING','EVALUATING','IMPORTED_MATCHED','IMPORTED_WARNING','IMPORTED_MISSING_ENTRY','NOT_FOUND','SKIPPED_EXISTING','SKIPPED_NOT_BETTER','ALREADY_SAME_BYTES','SOURCE_CHANGED','CATALOG_CHANGED','CATALOG_INVALID','VALIDATION_FAILED','READ_FAILED','INVALID_ARCHIVE','COMMIT_FAILED','CANCELLED')),
  candidate_count BIGINT NOT NULL DEFAULT 0 CHECK(candidate_count>=0),
  match_method TEXT CHECK(match_method IS NULL OR match_method IN ('EXACT_HASH','EXPECTED_SIZE_FALLBACK','LARGEST_SIZE_FALLBACK','DAT_ENTRY_MATCH','DAT_ENTRY_WARNING','DAT_PARTIAL_FALLBACK')),
  selection_details_json TEXT,
  previous_installation_id TEXT ,
  new_installation_id TEXT ,
  outcome_code TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms BIGINT,
  PRIMARY KEY(server_import_id,requirement_id),
  CHECK((source_kind='STATIC' AND dat_version_id IS NULL AND dat_machine_name IS NULL) OR
        (source_kind='DAT_MACHINE' AND dat_version_id IS NOT NULL AND dat_machine_name IS NOT NULL)),
  CHECK((state IN ('PENDING','EVALUATING'))=(completed_at_ms IS NULL))
);

CREATE TABLE import_job_files (
  import_job_id TEXT NOT NULL ,
  upload_file_id TEXT NOT NULL ,
  disposition TEXT NOT NULL CHECK(disposition IN ('PENDING','SOURCE','IGNORED','REJECTED')),
  reason_code TEXT,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  PRIMARY KEY(import_job_id, upload_file_id),
  CHECK((disposition IN ('IGNORED','REJECTED')) = (reason_code IS NOT NULL))
);

CREATE TABLE "import_job_file_resolutions" (
  import_job_id TEXT NOT NULL ,
  upload_file_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('RECONFIGURED')),
  replacement_import_job_id TEXT NOT NULL ,
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT ,
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','startup-test-bootstrap'
  )),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(import_job_id,upload_file_id),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);

CREATE TABLE import_items (
  id TEXT PRIMARY KEY,
  import_job_id TEXT NOT NULL ,
  group_key TEXT NOT NULL CHECK(length(group_key) = 64),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' ,
  state TEXT NOT NULL CHECK(state IN ('QUEUED','HASHING','IDENTIFYING','SCRAPING','REVIEW_PENDING','PUBLISHING','PUBLISHED','DISCARDED','FAILED_RETRYABLE','FAILED_FINAL','CANCELLED')),
  publication_game_id TEXT UNIQUE,
  publication_bulk_id TEXT,
  publication_json TEXT CHECK(publication_json IS NULL OR (publication_json IS JSON)),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest) = 64),
  search_text TEXT NOT NULL,
  failed_stage TEXT CHECK(failed_stage IS NULL OR failed_stage IN ('HASHING','IDENTIFYING','SCRAPING')),
  last_error_code TEXT,
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED')),
  payload_release_job_id TEXT UNIQUE ,
  payload_released_at_ms BIGINT,
  version BIGINT NOT NULL DEFAULT 1,
  target_platform_instance_id TEXT ,
  content_analysis_json TEXT NOT NULL DEFAULT '{}' CHECK((content_analysis_json IS JSON)),
  selected_candidate_id TEXT ,
  cover_candidate_asset_id TEXT ,
  background_candidate_asset_id TEXT ,
  cover_uploaded_asset_id TEXT ,
  video_uploaded_asset_id TEXT ,
  effective_source_snapshot_id TEXT ,
  default_dos_entry TEXT,
  metadata_json TEXT,
  review_profile_json TEXT CHECK(CASE WHEN review_profile_json IS NULL THEN true WHEN (review_profile_json IS JSON) THEN COALESCE(jsonb_typeof(((review_profile_json)::jsonb #> '{kind}'))='string' AND jsonb_typeof(((review_profile_json)::jsonb #> '{data}'))='object',false) ELSE false END),
  review_version BIGINT NOT NULL DEFAULT 0 CHECK(review_version>=0),
  review_created_at_ms BIGINT,
  review_updated_at_ms BIGINT,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  completed_at_ms BIGINT,
  UNIQUE(import_job_id, group_key),
  CHECK((state='PUBLISHING') = (publication_json IS NOT NULL)),
  CHECK(state<>'PUBLISHING' OR publication_game_id IS NOT NULL),
  CHECK((state IN ('FAILED_RETRYABLE','FAILED_FINAL')) = (failed_stage IS NOT NULL AND last_error_code IS NOT NULL)),
  CHECK((review_version=0 AND review_created_at_ms IS NULL AND review_updated_at_ms IS NULL)
    OR (review_version>0 AND review_created_at_ms IS NOT NULL AND review_updated_at_ms IS NOT NULL)),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL
  )
);

CREATE TABLE import_item_assets (
  import_item_id TEXT NOT NULL ,
  kind TEXT NOT NULL CHECK(kind IN ('COVER','VIDEO')),
  file_record TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK(length(media_type)>0),
  width_px BIGINT CHECK(width_px>0),
  height_px BIGINT CHECK(height_px>0),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(import_item_id,kind)
);

CREATE TABLE "import_item_source_files" (
  import_item_id TEXT NOT NULL ,
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC','PROJECT_FILE')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL ,
  file_record TEXT NOT NULL,
  source_archive_file_record TEXT,
  source_archive_entry_ordinal BIGINT,
  sort_order BIGINT NOT NULL CHECK(sort_order>=0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_item_id,role,logical_name),
  CHECK((source_archive_file_record IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE "import_item_source_snapshots" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL ,
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' ,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL
    CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  created_by TEXT NOT NULL CHECK(created_by IN ('IDENTIFICATION','ARCADE_PARENT_ATTACHMENT','MULTI_DISC_ATTACHMENT')),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(import_item_id,source_manifest_digest)
);

CREATE TABLE "import_item_source_snapshot_files" (
  source_snapshot_id TEXT NOT NULL ,
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC','PROJECT_FILE')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL ,
  file_record TEXT NOT NULL,
  source_archive_file_record TEXT,
  source_archive_entry_ordinal BIGINT,
  sort_order BIGINT NOT NULL CHECK(sort_order>=0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(source_snapshot_id,role,logical_name),
  CHECK((source_archive_file_record IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE "import_item_runtime_files" (
  import_item_id TEXT NOT NULL ,
  role TEXT NOT NULL CHECK(role IN (
    'DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
  logical_name TEXT NOT NULL,
  file_record TEXT NOT NULL,
  sort_order BIGINT NOT NULL CHECK(sort_order>=0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_item_id,role,logical_name)
);

CREATE TABLE import_item_dos_entries (
  import_item_id TEXT NOT NULL ,
  normalized_path TEXT NOT NULL,
  original_relative_path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('EXE','COM','BAT')),
  rank BIGINT NOT NULL,
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  direct_launch_safe BIGINT NOT NULL CHECK(direct_launch_safe IN (0,1)),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(import_item_id, normalized_path)
);

CREATE TABLE import_item_multidisc_entries (
  source_snapshot_id TEXT NOT NULL ,
  ordinal BIGINT NOT NULL CHECK(ordinal BETWEEN 0 AND 7),
  source_reference TEXT NOT NULL,
  normalized_reference TEXT NOT NULL,
  canonical_name TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('PRESENT','MISSING','RELEASED')),
  upload_file_id TEXT ,
  file_record TEXT,
  source_logical_name TEXT,
  payload_released_at_ms BIGINT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(source_snapshot_id,ordinal),
  UNIQUE(source_snapshot_id,normalized_reference),
  UNIQUE(source_snapshot_id,canonical_name),
  CHECK(octet_length(source_reference) BETWEEN 1 AND 255),
  CHECK(octet_length(normalized_reference) BETWEEN 1 AND 255),
  CHECK(canonical_name=('disc-' || lpad((ordinal+1)::text,3,'0') || '.chd')),
  CHECK(
    state='PRESENT' AND upload_file_id IS NOT NULL AND file_record IS NOT NULL AND source_logical_name IS NOT NULL AND payload_released_at_ms IS NULL OR
    state='MISSING' AND upload_file_id IS NULL AND file_record IS NULL AND source_logical_name IS NULL AND payload_released_at_ms IS NULL OR
    state='RELEASED' AND upload_file_id IS NULL AND file_record IS NULL AND payload_released_at_ms IS NOT NULL
  )
);

CREATE TABLE review_uploaded_assets (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL ,
  upload_file_id TEXT NOT NULL UNIQUE ,
  file_record TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('COVER','VIDEO')),
  width_px BIGINT,
  height_px BIGINT,
  media_type TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL,
  CHECK ((kind='COVER' AND width_px IS NOT NULL AND width_px>0
    AND height_px IS NOT NULL AND height_px>0
    AND media_type IN ('image/png','image/jpeg','image/webp'))
    OR (kind='VIDEO' AND width_px IS NULL AND height_px IS NULL
    AND media_type IN ('video/mp4','video/webm')))
);

CREATE TABLE review_multidisc_attachments (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL ,
  review_draft_id TEXT NOT NULL ,
  requested_by_user_id TEXT NOT NULL ,
  base_source_snapshot_id TEXT NOT NULL ,
  result_source_snapshot_id TEXT ,
  upload_session_id TEXT NOT NULL ,
  expected_set_digest TEXT NOT NULL
    CHECK(length(expected_set_digest)=64 AND expected_set_digest=lower(expected_set_digest)),
  state TEXT NOT NULL CHECK(state IN ('PENDING','ACCEPTED','REJECTED','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(octet_length(diagnostics_json)<=65536),
  job_id TEXT NOT NULL UNIQUE ,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  finished_at_ms BIGINT,
  CHECK(
    state='ACCEPTED' AND result_source_snapshot_id IS NOT NULL OR
    state<>'ACCEPTED' AND result_source_snapshot_id IS NULL
  ),
  CHECK((state IN ('REJECTED','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','CANCELLED'))=(finished_at_ms IS NOT NULL))
);

CREATE TABLE review_draft_screenshot_assets (
  review_draft_id TEXT NOT NULL ,
  ordinal BIGINT NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  candidate_asset_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(review_draft_id, ordinal),
  UNIQUE(review_draft_id, candidate_asset_id)
);

CREATE TABLE review_bulk_approvals (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL UNIQUE ,
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','COMPLETED','FAILED')),
  max_item_id TEXT NOT NULL ,
  cursor_item_id TEXT,
  initial_pending_count BIGINT NOT NULL CHECK(initial_pending_count BETWEEN 1 AND 10000),
  scanned_count BIGINT NOT NULL DEFAULT 0 CHECK(scanned_count>=0),
  published_count BIGINT NOT NULL DEFAULT 0 CHECK(published_count>=0),
  skipped_duplicate_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_duplicate_count>=0),
  skipped_changed_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_changed_count>=0),
  skipped_not_ready_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_not_ready_count>=0),
  created_by_user_id TEXT NOT NULL ,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  last_error_code TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  started_at_ms BIGINT,
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms BIGINT,
  CHECK((state IN ('COMPLETED','FAILED'))=(completed_at_ms IS NOT NULL)),
  CHECK(cursor_item_id IS NULL OR cursor_item_id<=max_item_id),
  CHECK(scanned_count=published_count+skipped_duplicate_count+skipped_changed_count+
    skipped_not_ready_count)
);

CREATE TABLE review_draft_tags (
  review_draft_id TEXT NOT NULL ,
  tag_id TEXT NOT NULL ,
  assigned_by_user_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(review_draft_id,tag_id)
);

CREATE TABLE metadata_provider_cache (
  provider TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  current_response_id TEXT NOT NULL ,
  expires_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  PRIMARY KEY(provider, request_digest)
);

CREATE TABLE metadata_provider_responses (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL CHECK(provider = 'HASHEOUS'),
  request_digest TEXT NOT NULL CHECK(length(request_digest) = 64),
  http_status BIGINT,
  outcome TEXT NOT NULL CHECK(outcome IN ('HIT','MISS','RATE_LIMITED','TIMEOUT','INVALID_RESPONSE','NETWORK_ERROR')),
  raw_response_file_record TEXT,
  raw_payload_state TEXT NOT NULL CHECK(raw_payload_state IN ('NONE','RETAINED','RELEASED')),
  raw_payload_released_at_ms BIGINT,
  fetched_at_ms BIGINT NOT NULL,
  expires_at_ms BIGINT NOT NULL,
  CHECK(
    raw_payload_state='NONE' AND raw_response_file_record IS NULL AND raw_payload_released_at_ms IS NULL OR
    raw_payload_state='RETAINED' AND raw_response_file_record IS NOT NULL AND raw_payload_released_at_ms IS NULL OR
    raw_payload_state='RELEASED' AND raw_response_file_record IS NULL AND raw_payload_released_at_ms IS NOT NULL
  )
);

CREATE TABLE metadata_scrape_query_attempts (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL ,
  content_hash_evidence_id TEXT NOT NULL ,
  provider_response_id TEXT NOT NULL ,
  attempt_no BIGINT NOT NULL CHECK(attempt_no >= 1),
  source TEXT NOT NULL CHECK(source IN ('NETWORK','CACHE')),
  created_at_ms BIGINT NOT NULL,
  UNIQUE(content_hash_evidence_id, attempt_no)
);

CREATE TABLE scrape_candidates (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL ,
  primary_response_id TEXT NOT NULL ,
  provider_game_id TEXT NOT NULL,
  normalized_metadata_json TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL,
  UNIQUE(scrape_run_id, provider_game_id)
);

CREATE TABLE scrape_candidate_hits (
  scrape_candidate_id TEXT NOT NULL ,
  query_attempt_id TEXT NOT NULL ,
  matched_hashes_json TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(scrape_candidate_id, query_attempt_id)
);

CREATE TABLE scrape_candidate_assets (
  id TEXT PRIMARY KEY,
  scrape_candidate_id TEXT NOT NULL ,
  provider_response_id TEXT NOT NULL ,
  provider_asset_id TEXT NOT NULL,
  kind_hint TEXT NOT NULL CHECK(kind_hint IN ('COVER','BACKGROUND','SCREENSHOT','UNKNOWN')),
  ordinal BIGINT NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  source_path TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('PENDING','FETCHING','READY','FAILED','CANCELLED')),
  file_record TEXT,
  width_px BIGINT,
  height_px BIGINT,
  media_type TEXT,
  error_code TEXT,
  fetched_at_ms BIGINT,
  media_fetch_job_id TEXT ,
  media_fetch_order BIGINT CHECK(media_fetch_order IS NULL OR media_fetch_order>=0),
  media_charged_bytes BIGINT NOT NULL DEFAULT 0 CHECK(media_charged_bytes BETWEEN 0 AND 104857600),
  media_reserved_bytes BIGINT NOT NULL DEFAULT 0 CHECK(media_reserved_bytes BETWEEN 0 AND 10485761 AND media_reserved_bytes<=media_charged_bytes),
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  UNIQUE(scrape_candidate_id, provider_asset_id),
  CHECK((status = 'READY') = (file_record IS NOT NULL AND width_px IS NOT NULL AND height_px IS NOT NULL AND media_type IS NOT NULL)),
  CHECK((status IN ('FAILED','CANCELLED')) = (error_code IS NOT NULL))
);

CREATE TABLE content_hash_evidence (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL ,
  profile TEXT NOT NULL CHECK(
    length(profile) BETWEEN 2 AND 64 AND profile=upper(profile)
    AND profile !~ '^.*[^A-Z0-9_].*$'
  ),
  file_record TEXT,
  archive_file_record TEXT,
  archive_entry_ordinal BIGINT,
  payload_released_at_ms BIGINT,
  crc32 TEXT,
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  query_order BIGINT NOT NULL CHECK(query_order >= 0),
  created_at_ms BIGINT NOT NULL,
  UNIQUE(scrape_run_id, profile, query_order),
  CHECK(
    payload_released_at_ms IS NULL AND ((file_record IS NOT NULL) != (archive_file_record IS NOT NULL)) OR
    payload_released_at_ms IS NOT NULL AND file_record IS NULL AND archive_file_record IS NULL AND archive_entry_ordinal IS NULL
  ),
  CHECK(crc32 IS NOT NULL OR md5 IS NOT NULL OR sha1 IS NOT NULL OR sha256 IS NOT NULL)
);

CREATE TABLE content_identity_claims (
  platform_id TEXT NOT NULL ,
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(platform_id, content_identity_digest)
);

CREATE TABLE import_group_requests (
  import_job_id TEXT PRIMARY KEY ,
  schema_version BIGINT NOT NULL CHECK(schema_version=1),
  request_json TEXT NOT NULL CHECK((request_json IS JSON)),
  request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest=lower(request_digest)),
  actor_user_id TEXT ,
  upload_version BIGINT NOT NULL CHECK(upload_version>=1),
  upload_manifest_digest TEXT NOT NULL CHECK(length(upload_manifest_digest)=64 AND upload_manifest_digest=lower(upload_manifest_digest)),
  target_snapshot_json TEXT NOT NULL CHECK((target_snapshot_json IS JSON)),
  target_snapshot_digest TEXT NOT NULL CHECK(length(target_snapshot_digest)=64 AND target_snapshot_digest=lower(target_snapshot_digest)),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0)
);

CREATE TABLE "import_jobs" (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL UNIQUE ,
  target_platform_instance_id TEXT NOT NULL ,
  platform_instance_version BIGINT NOT NULL,
  platform_id TEXT NOT NULL ,
  default_core_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  dat_version_id TEXT,
  metadata_provider TEXT NOT NULL CHECK(metadata_provider IN ('HASHEOUS','NONE')),
  config_snapshot_json TEXT NOT NULL,
  config_snapshot_digest TEXT NOT NULL CHECK(length(config_snapshot_digest) = 64),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','REVIEW_PENDING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED')),
  total_item_count BIGINT NOT NULL DEFAULT 0,
  queued_item_count BIGINT NOT NULL DEFAULT 0,
  running_item_count BIGINT NOT NULL DEFAULT 0,
  review_pending_item_count BIGINT NOT NULL DEFAULT 0,
  published_item_count BIGINT NOT NULL DEFAULT 0,
  discarded_item_count BIGINT NOT NULL DEFAULT 0,
  failed_item_count BIGINT NOT NULL DEFAULT 0,
  cancelled_item_count BIGINT NOT NULL DEFAULT 0,
  ignored_file_count BIGINT NOT NULL DEFAULT 0,
  rejected_file_count BIGINT NOT NULL DEFAULT 0,
  last_error_code TEXT,
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED')),
  payload_release_job_id TEXT UNIQUE ,
  payload_released_at_ms BIGINT,
  cancel_requested_at_ms BIGINT,
  cancel_reason TEXT,
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  completed_at_ms BIGINT,
  resolved_rejected_file_count BIGINT NOT NULL DEFAULT 0
CHECK(resolved_rejected_file_count BETWEEN 0 AND rejected_file_count),
  reconfigured_from_import_job_id TEXT ,
  already_imported_item_count BIGINT NOT NULL DEFAULT 0
CHECK(already_imported_item_count BETWEEN 0 AND discarded_item_count),
  already_imported_file_count BIGINT NOT NULL DEFAULT 0
CHECK(already_imported_file_count >= 0),
  CHECK(total_item_count = queued_item_count + running_item_count + review_pending_item_count + published_item_count + discarded_item_count + failed_item_count + cancelled_item_count),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL
  )
);

CREATE TABLE "import_item_duplicate_matches" (
  import_item_id TEXT NOT NULL ,
  existing_game_id TEXT NOT NULL ,
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  detected_stage TEXT NOT NULL CHECK(detected_stage = 'IDENTIFICATION'),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(import_item_id, existing_game_id)
);

CREATE TABLE "review_arcade_parent_attachments" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL ,
  review_draft_id TEXT NOT NULL ,
  base_source_snapshot_id TEXT NOT NULL ,
  result_source_snapshot_id TEXT ,
  dependency_machine TEXT NOT NULL CHECK(octet_length(dependency_machine) BETWEEN 1 AND 255),
  expected_logical_name TEXT NOT NULL,
  required_by_machine TEXT NOT NULL CHECK(octet_length(required_by_machine) BETWEEN 1 AND 255),
  depth BIGINT NOT NULL CHECK(depth BETWEEN 1 AND 63),
  provider_id TEXT NOT NULL ,
  target_id TEXT NOT NULL,
  dat_version_id TEXT NOT NULL ,
  upload_file_id TEXT ,
  accepted_file_record TEXT,
  payload_released_at_ms BIGINT,
  original_filename TEXT NOT NULL CHECK(octet_length(original_filename) BETWEEN 1 AND 255),
  observed_size_bytes BIGINT CHECK(observed_size_bytes IS NULL OR observed_size_bytes >= 0),
  observed_sha256 TEXT CHECK(observed_sha256 IS NULL OR (length(observed_sha256)=64 AND observed_sha256=lower(observed_sha256))),
  state TEXT NOT NULL CHECK(state IN ('PENDING','ACCEPTED','REJECTED','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(octet_length(diagnostics_json) <= 65536),
  job_id TEXT NOT NULL UNIQUE ,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms >= created_at_ms),
  finished_at_ms BIGINT,
  CHECK(expected_logical_name=dependency_machine||'.zip'),
  CHECK((state='ACCEPTED')=(result_source_snapshot_id IS NOT NULL)),
  CHECK(state<>'ACCEPTED' AND accepted_file_record IS NULL AND payload_released_at_ms IS NULL OR
        state='ACCEPTED' AND accepted_file_record IS NOT NULL AND payload_released_at_ms IS NULL OR
        state='ACCEPTED' AND accepted_file_record IS NULL AND payload_released_at_ms IS NOT NULL),
  CHECK((state IN ('REJECTED','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK((observed_size_bytes IS NULL)=(observed_sha256 IS NULL))
);

CREATE TABLE review_preview_bindings (
  preview_session_id TEXT PRIMARY KEY ,
  import_item_id TEXT NOT NULL ,
  source_snapshot_id TEXT NOT NULL 
);

CREATE TABLE "review_runtime_screenshots" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL ,
  preview_session_id TEXT NOT NULL ,
  source_snapshot_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  file_record TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK(media_type IN ('image/png','image/jpeg')),
  width_px BIGINT NOT NULL CHECK(width_px BETWEEN 1 AND 40000000),
  height_px BIGINT NOT NULL CHECK(height_px BETWEEN 1 AND 40000000),
  captured_at_ms BIGINT NOT NULL CHECK(captured_at_ms>=0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=0),
  UNIQUE(import_item_id)
);

CREATE TABLE "metadata_scrape_runs" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT ,
  game_id TEXT ,
  job_id TEXT NOT NULL UNIQUE ,
  provider TEXT NOT NULL CHECK(provider IN ('HASHEOUS','NONE')),
  provider_config_version BIGINT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('RUNNING','COMPLETED','FAILED','CANCELLED')),
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  completed_at_ms BIGINT,
  error_code TEXT,
  UNIQUE(import_item_id),
  UNIQUE(game_id),
  CHECK((import_item_id IS NOT NULL) != (game_id IS NOT NULL)),
  CHECK((state = 'RUNNING') = (completed_at_ms IS NULL)),
  CHECK((state = 'FAILED') = (error_code IS NOT NULL))
);

CREATE TABLE "dos_entries" (
  game_id TEXT NOT NULL ,
  normalized_path TEXT NOT NULL,
  original_relative_path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('EXE','COM','BAT')),
  rank BIGINT NOT NULL,
  enabled BIGINT NOT NULL CHECK(enabled IN (0,1)),
  direct_launch_safe BIGINT NOT NULL CHECK(direct_launch_safe IN (0,1)),
  PRIMARY KEY(game_id, normalized_path)
);

CREATE TABLE tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 40 AND octet_length(name)<=160),
  name_key TEXT NOT NULL CHECK(length(name_key)>=1 AND octet_length(name_key)<=160),
  search_text TEXT NOT NULL CHECK(length(search_text)>=1 AND octet_length(search_text)<=160),
  status TEXT NOT NULL CHECK(status IN ('ACTIVE','DELETED')),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_by_user_id TEXT NOT NULL ,
  updated_by_user_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  deleted_at_ms BIGINT,
  CHECK((status='DELETED')=(deleted_at_ms IS NOT NULL)),
  CHECK(length(id)=36 AND lower(id)=id
    AND id !~ '^.*[^0-9a-f-].*$'
    AND substr(id,9,1)='-' AND substr(id,14,1)='-'
    AND substr(id,19,1)='-' AND substr(id,24,1)='-')
);

CREATE TABLE game_tags (
  game_id TEXT NOT NULL ,
  tag_id TEXT NOT NULL ,
  assigned_by_user_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(game_id,tag_id)
);

CREATE TABLE favorite_games (
  profile_id TEXT NOT NULL ,
  game_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(profile_id,game_id),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id !~ '^.*[^0-9a-f-].*$'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-'),
  CHECK(length(game_id)=36 AND lower(game_id)=game_id
    AND game_id !~ '^.*[^0-9a-f-].*$'
    AND substr(game_id,9,1)='-' AND substr(game_id,14,1)='-'
    AND substr(game_id,19,1)='-' AND substr(game_id,24,1)='-')
);

CREATE TABLE favorite_folders (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL ,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 40 AND octet_length(name)<=160),
  name_key TEXT NOT NULL CHECK(length(name_key)>=1 AND octet_length(name_key)<=160),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(profile_id,id),
  UNIQUE(profile_id,name_key),
  CHECK(length(id)=36 AND lower(id)=id
    AND id !~ '^.*[^0-9a-f-].*$'
    AND substr(id,9,1)='-' AND substr(id,14,1)='-'
    AND substr(id,19,1)='-' AND substr(id,24,1)='-'),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id !~ '^.*[^0-9a-f-].*$'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-')
);

CREATE TABLE favorite_folder_games (
  profile_id TEXT NOT NULL,
  folder_id TEXT NOT NULL,
  game_id TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(profile_id,folder_id,game_id),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id !~ '^.*[^0-9a-f-].*$'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-'),
  CHECK(length(folder_id)=36 AND lower(folder_id)=folder_id
    AND folder_id !~ '^.*[^0-9a-f-].*$'
    AND substr(folder_id,9,1)='-' AND substr(folder_id,14,1)='-'
    AND substr(folder_id,19,1)='-' AND substr(folder_id,24,1)='-'),
  CHECK(length(game_id)=36 AND lower(game_id)=game_id
    AND game_id !~ '^.*[^0-9a-f-].*$'
    AND substr(game_id,9,1)='-' AND substr(game_id,14,1)='-'
    AND substr(game_id,19,1)='-' AND substr(game_id,24,1)='-')
);

CREATE TABLE "game_assets" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL ,
  file_record TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('COVER','BACKGROUND','SCREENSHOT','VIDEO')),
  ordinal BIGINT NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  width_px BIGINT,
  height_px BIGINT,
  media_type TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(game_id,kind,ordinal),
  CHECK(
    (kind IN ('COVER','BACKGROUND','SCREENSHOT') AND width_px>0 AND height_px>0 AND media_type IN ('image/png','image/jpeg','image/webp')) OR
    (kind='VIDEO' AND ordinal=0 AND width_px IS NULL AND height_px IS NULL AND media_type IN ('video/mp4','video/webm'))
  )
);

CREATE TABLE "game_variants" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL ,
  core_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL ,
  target_id TEXT NOT NULL,
  dat_version_id TEXT ,
  emulator_game_id BIGINT UNIQUE CHECK(emulator_game_id IS NULL OR emulator_game_id BETWEEN 1 AND 9007199254740991),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  runtime_profile_json TEXT CHECK(CASE WHEN runtime_profile_json IS NULL THEN true WHEN (runtime_profile_json IS JSON) THEN COALESCE(jsonb_typeof(((runtime_profile_json)::jsonb #> '{kind}'))='string' AND jsonb_typeof(((runtime_profile_json)::jsonb #> '{data}'))='object',false) ELSE false END),
  default_dos_entry TEXT,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(game_id,core_id),
  CHECK(status='READY' OR emulator_game_id IS NULL)
);

CREATE TABLE "games" (
  id TEXT PRIMARY KEY,
  platform_instance_id TEXT NOT NULL ,
  title TEXT NOT NULL CHECK(length(title)>0),
  title_initial TEXT NOT NULL CHECK(
    length(title_initial)=1 AND (title_initial='#' OR title_initial ~ '^[0-9]$' OR title_initial ~ '^[A-Z]$')
  ),
  description TEXT NOT NULL,
  developer TEXT NOT NULL,
  publisher TEXT NOT NULL,
  genre TEXT NOT NULL,
  players BIGINT CHECK(players IS NULL OR players BETWEEN 1 AND 64),
  release_year BIGINT,
  metadata_source_kind TEXT NOT NULL CHECK(metadata_source_kind IN (
    'IMPORT_REVIEW','ADMIN_EDIT','RESCRAPE_APPLY','IMPORT_RECEIVE'
  )),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' ,
  content_source_kind TEXT NOT NULL CHECK(content_source_kind IN (
    'IMPORT_REVIEW','ADMIN_REPLACE','IMPORT_RECEIVE'
  )),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64),
  content_profile_json TEXT CHECK(CASE WHEN content_profile_json IS NULL THEN true WHEN (content_profile_json IS JSON) THEN COALESCE(jsonb_typeof(((content_profile_json)::jsonb #> '{kind}'))='string' AND jsonb_typeof(((content_profile_json)::jsonb #> '{data}'))='object',false) ELSE false END),
  status TEXT NOT NULL CHECK(status IN ('PUBLISHED','DELETED')),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED')),
  payload_release_job_id TEXT UNIQUE ,
  payload_released_at_ms BIGINT,
  search_text TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  deleted_at_ms BIGINT,
  CHECK((status='DELETED')=(deleted_at_ms IS NOT NULL)),
  CHECK(status<>'PUBLISHED' OR payload_state='RETAINED'),
  CHECK(status<>'DELETED' OR payload_state IN ('RELEASING','RELEASED')),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL
  )
);

CREATE TABLE "variant_dependencies" (
  game_variant_id TEXT NOT NULL ,
  kind TEXT NOT NULL CHECK(kind IN ('PARENT','BIOS_OR_BASE')),
  logical_archive TEXT NOT NULL,
  dat_version_id TEXT NOT NULL ,
  source_machine_name TEXT NOT NULL,
  required_entries_json TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('SATISFIED_BY_CONTENT','SATISFIED_EXTERNAL','HASH_WARNING','MISSING','MISMATCH','UNSUPPORTED')),
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY(game_variant_id,kind,logical_archive),
  CHECK(kind='BIOS_OR_BASE' OR state<>'HASH_WARNING')
);

CREATE TABLE "variant_files" (
  game_variant_id TEXT NOT NULL ,
  role TEXT NOT NULL CHECK(role IN (
    'PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
  logical_name TEXT NOT NULL,
  file_record TEXT NOT NULL,
  sort_order BIGINT NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_variant_id,role,logical_name)
);

CREATE TABLE "game_files" (
  game_id TEXT NOT NULL ,
  role TEXT NOT NULL CHECK(role IN (
    'CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC','PROJECT_FILE',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
  logical_name TEXT NOT NULL,
  file_record TEXT NOT NULL,
  source_archive_file_record TEXT,
  source_archive_entry_ordinal BIGINT,
  sort_order BIGINT NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_id,role,logical_name),
  CHECK((source_archive_file_record IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE "source_imports" (
  id TEXT PRIMARY KEY,
  format TEXT NOT NULL DEFAULT 'PEGASUS' CHECK(format IN ('BASIC','PEGASUS','GAMELIST')),
  extension_filter TEXT NOT NULL DEFAULT '' CHECK(length(extension_filter)<=2048 AND (format='BASIC' OR extension_filter='')),
  root_id TEXT NOT NULL CHECK(octet_length(root_id) BETWEEN 1 AND 32),
  root_label_snapshot TEXT NOT NULL CHECK(length(root_label_snapshot) BETWEEN 1 AND 40 AND octet_length(root_label_snapshot)<=160),
  source_relative_path TEXT NOT NULL CHECK(octet_length(source_relative_path)<=4096),
  root_config_digest TEXT NOT NULL CHECK(length(root_config_digest)=64 AND root_config_digest=lower(root_config_digest)),
  source_snapshot_digest TEXT CHECK(source_snapshot_digest IS NULL OR (length(source_snapshot_digest)=64 AND source_snapshot_digest=lower(source_snapshot_digest))),
  state TEXT NOT NULL CHECK(state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED','EXPIRED')),
  phase TEXT CHECK(phase IS NULL OR phase IN ('DISCOVERING_METADATA','PARSING_METADATA','RESOLVING_SOURCES','COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS')),
  scan_job_id TEXT NOT NULL UNIQUE ,
  import_job_id TEXT UNIQUE ,
  scan_outcome TEXT NOT NULL DEFAULT 'PENDING' CHECK(scan_outcome IN ('PENDING','READY','PARTIAL','INVALID','EMPTY','NO_METADATA')),
  scan_diagnostics_json TEXT NOT NULL DEFAULT '[]' CHECK((scan_diagnostics_json IS JSON) AND jsonb_typeof((scan_diagnostics_json)::jsonb)='array' AND jsonb_array_length((scan_diagnostics_json)::jsonb)<=100),
  metadata_count BIGINT NOT NULL DEFAULT 0 CHECK(metadata_count>=0),
  invalid_metadata_count BIGINT NOT NULL DEFAULT 0 CHECK(invalid_metadata_count>=0),
  collection_count BIGINT NOT NULL DEFAULT 0 CHECK(collection_count>=0),
  game_count BIGINT NOT NULL DEFAULT 0 CHECK(game_count>=0),
  estimated_source_bytes BIGINT NOT NULL DEFAULT 0 CHECK(estimated_source_bytes>=0),
  mapped_collection_count BIGINT NOT NULL DEFAULT 0 CHECK(mapped_collection_count>=0),
  skipped_collection_count BIGINT NOT NULL DEFAULT 0 CHECK(skipped_collection_count>=0),
  processable_item_count BIGINT NOT NULL DEFAULT 0 CHECK(processable_item_count>=0),
  blocked_item_count BIGINT NOT NULL DEFAULT 0 CHECK(blocked_item_count>=0),
  review_pending_item_count BIGINT NOT NULL DEFAULT 0 CHECK(review_pending_item_count>=0),
  published_item_count BIGINT NOT NULL DEFAULT 0 CHECK(published_item_count>=0),
  review_discarded_item_count BIGINT NOT NULL DEFAULT 0 CHECK(review_discarded_item_count>=0),
  existing_item_count BIGINT NOT NULL DEFAULT 0 CHECK(existing_item_count>=0),
  failed_item_count BIGINT NOT NULL DEFAULT 0 CHECK(failed_item_count>=0),
  cancelled_item_count BIGINT NOT NULL DEFAULT 0 CHECK(cancelled_item_count>=0),
  media_warning_count BIGINT NOT NULL DEFAULT 0 CHECK(media_warning_count>=0),
  discovered_cover_count BIGINT NOT NULL DEFAULT 0 CHECK(discovered_cover_count>=0),
  discovered_video_count BIGINT NOT NULL DEFAULT 0 CHECK(discovered_video_count>=0),
  mapping_version BIGINT NOT NULL DEFAULT 1 CHECK(mapping_version>=1),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_by_user_id TEXT NOT NULL ,
  last_error_code TEXT,
  retryable BIGINT NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  cancel_reason TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  scan_completed_at_ms BIGINT,
  started_at_ms BIGINT,
  completed_at_ms BIGINT,
  expires_at_ms BIGINT NOT NULL CHECK(expires_at_ms>=created_at_ms),
  CHECK((state IN ('PARTIAL_FAILURE','COMPLETED','CANCELLED','FAILED','EXPIRED'))=(completed_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_reason IS NOT NULL)),
  CHECK(mapped_collection_count+skipped_collection_count<=collection_count),
  CHECK(review_pending_item_count+published_item_count+review_discarded_item_count+existing_item_count+blocked_item_count+failed_item_count+cancelled_item_count<=game_count)
);

CREATE TABLE source_import_metadata_files (
  import_id TEXT NOT NULL ,
  relative_path TEXT NOT NULL CHECK(octet_length(relative_path) BETWEEN 1 AND 4096),
  size_bytes BIGINT NOT NULL CHECK(size_bytes>=0),
  content_digest TEXT CHECK(content_digest IS NULL OR (length(content_digest)=64 AND content_digest=lower(content_digest))),
  source_facts_digest TEXT NOT NULL CHECK(length(source_facts_digest)=64 AND source_facts_digest=lower(source_facts_digest)),
  parse_state TEXT NOT NULL CHECK(parse_state IN ('VALID','INVALID')),
  error_code TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_id,relative_path),
  CHECK((parse_state='INVALID')=(error_code IS NOT NULL)),
  CHECK((size_bytes<=8388608 AND content_digest IS NOT NULL) OR
    (size_bytes>8388608 AND content_digest IS NULL AND parse_state='INVALID'
     AND error_code IN ('PEGASUS_METADATA_TOO_LARGE','EMULATIONSTATION_GAMELIST_TOO_LARGE')))
);

CREATE TABLE source_collection_tags (
  collection_id TEXT NOT NULL ,
  tag_id TEXT NOT NULL ,
  assigned_by_user_id TEXT NOT NULL ,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(collection_id,tag_id)
);

CREATE TABLE source_import_item_files (
  item_id TEXT NOT NULL ,
  ordinal BIGINT NOT NULL CHECK(ordinal>=0 AND ordinal<64),
  declared_kind TEXT NOT NULL CHECK(declared_kind IN ('FILE','PLAYLIST','DISC')),
  relative_path TEXT NOT NULL CHECK(octet_length(relative_path) BETWEEN 1 AND 4096),
  size_bytes BIGINT CHECK(size_bytes IS NULL OR size_bytes>=0),
  source_facts_digest TEXT CHECK(source_facts_digest IS NULL OR (length(source_facts_digest)=64 AND source_facts_digest=lower(source_facts_digest))),
  file_record TEXT,
  source_archive_file_record TEXT,
  source_archive_entry_ordinal BIGINT,
  role TEXT CHECK(role IS NULL OR role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT,
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','COPIED','SOURCE_CHANGED','READ_FAILED','UNSUPPORTED','RELEASED')),
  payload_released_at_ms BIGINT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(item_id,ordinal),
  UNIQUE(item_id,relative_path),
  CHECK((source_archive_file_record IS NULL)=(source_archive_entry_ordinal IS NULL)),
  CHECK(state='RELEASED' AND file_record IS NULL AND source_archive_file_record IS NULL AND payload_released_at_ms IS NOT NULL OR
        state<>'RELEASED' AND payload_released_at_ms IS NULL)
);

CREATE TABLE source_import_item_assets (
  item_id TEXT NOT NULL ,
  kind TEXT NOT NULL CHECK(kind IN ('COVER','VIDEO')),
  resolution_method TEXT NOT NULL CHECK(resolution_method IN ('EXPLICIT_GAME','EXPLICIT_COLLECTION','AUTO_TITLE','AUTO_FILE')),
  relative_path TEXT NOT NULL CHECK(octet_length(relative_path) BETWEEN 1 AND 4096),
  size_bytes BIGINT CHECK(size_bytes IS NULL OR size_bytes>=0),
  source_facts_digest TEXT CHECK(source_facts_digest IS NULL OR (length(source_facts_digest)=64 AND source_facts_digest=lower(source_facts_digest))),
  file_record TEXT,
  media_type TEXT,
  width_px BIGINT,
  height_px BIGINT,
  state TEXT NOT NULL CHECK(state IN ('DISCOVERED','COPIED','MISSING','AMBIGUOUS','INVALID','TOO_LARGE','SOURCE_CHANGED','READ_FAILED','RELEASED')),
  payload_released_at_ms BIGINT,
  warning_code TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(item_id,kind),
  CHECK(kind<>'COVER' OR media_type IS NULL OR (media_type IN ('image/png','image/jpeg','image/webp') AND width_px>0 AND height_px>0)),
  CHECK(kind<>'VIDEO' OR media_type IS NULL OR (media_type IN ('video/mp4','video/webm') AND width_px IS NULL AND height_px IS NULL)),
  CHECK(state='RELEASED' AND file_record IS NULL AND payload_released_at_ms IS NOT NULL OR
        state<>'RELEASED' AND payload_released_at_ms IS NULL)
);

CREATE TABLE "source_import_collections" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL ,
  metadata_relative_path TEXT NOT NULL CHECK(octet_length(metadata_relative_path) BETWEEN 1 AND 4096),
  segment_ordinal BIGINT NOT NULL CHECK(segment_ordinal>=0),
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
  shortname TEXT,
  description TEXT NOT NULL DEFAULT '',
  game_count BIGINT NOT NULL CHECK(game_count>=0),
  issue_count BIGINT NOT NULL DEFAULT 0 CHECK(issue_count>=0),
  ignored_rules_json TEXT NOT NULL DEFAULT '[]',
  warning_fields_json TEXT NOT NULL DEFAULT '[]',
  mapping_action TEXT CHECK(mapping_action IS NULL OR mapping_action IN ('IMPORT','SKIP')),
  target_platform_instance_id TEXT ,
  target_platform_instance_version BIGINT CHECK(target_platform_instance_version IS NULL OR target_platform_instance_version>=1),
  target_platform_id TEXT ,
  target_default_core_id TEXT ,
  target_provider_id TEXT,
  target_id TEXT,
  target_dat_version_id TEXT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  tag_snapshot_json TEXT NOT NULL DEFAULT '[]'
CHECK((tag_snapshot_json IS JSON) AND jsonb_typeof((tag_snapshot_json)::jsonb)='array'),
  UNIQUE(import_id,metadata_relative_path,segment_ordinal),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_version IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_default_core_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_provider_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_id IS NOT NULL))
);

CREATE TABLE "source_import_items" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL ,
  collection_id TEXT ,
  metadata_relative_path TEXT NOT NULL CHECK(octet_length(metadata_relative_path) BETWEEN 1 AND 4096),
  game_ordinal BIGINT NOT NULL CHECK(game_ordinal>=0),
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key=lower(source_key)),
  title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  discovery_state TEXT NOT NULL CHECK(discovery_state IN ('READY','BLOCKED_SOURCE','BLOCKED_CONTENT')),
  execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  content_kind TEXT ,
  metadata_json TEXT NOT NULL,
  warnings_json TEXT NOT NULL DEFAULT '[]',
  source_flags_json TEXT NOT NULL DEFAULT '{}' CHECK((source_flags_json IS JSON)),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  discovery_code TEXT,
  error_code TEXT,
  retryable BIGINT NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED')),
  payload_release_job_id TEXT ,
  payload_released_at_ms BIGINT,
  library_import_job_id TEXT ,
  library_import_item_id TEXT UNIQUE ,
  published_game_id TEXT ,
  existing_game_id TEXT ,
  existing_matches_json TEXT NOT NULL DEFAULT '[]' CHECK((existing_matches_json IS JSON) AND jsonb_typeof((existing_matches_json)::jsonb)='array'),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms BIGINT,
  error_details_json TEXT CHECK(
    error_details_json IS NULL OR (
      (error_details_json IS JSON) AND jsonb_typeof((error_details_json)::jsonb)='object'
      AND octet_length(error_details_json)<=8192
    )
  ),
  UNIQUE(import_id,source_key),
  UNIQUE(import_id,metadata_relative_path,game_ordinal),
  CHECK((execution_state IN ('PENDING','COPYING','VALIDATING'))=(completed_at_ms IS NULL)),
  CHECK((execution_state='PUBLISHED')=(published_game_id IS NOT NULL)),
  CHECK((execution_state='SKIPPED_EXISTING')=(existing_game_id IS NOT NULL)),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL
  )
);

CREATE TABLE source_import_item_companions (
  item_id TEXT NOT NULL ,
  candidate_item_id TEXT NOT NULL ,
  file_record TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL,
  PRIMARY KEY (item_id,candidate_item_id)
);

CREATE TABLE runtime_preview_files (
  preview_session_id TEXT NOT NULL ,
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','EXTERNAL_FILE','DISC','PROJECT_FILE','RUNTIME_FILE')),
  logical_name TEXT NOT NULL CHECK(
    octet_length(logical_name) BETWEEN 1 AND 1024 AND
    strpos(logical_name,chr(92))=0 AND logical_name NOT IN ('.','..') AND
    true AND
    (role IN ('PROJECT_FILE','RUNTIME_FILE') OR logical_name NOT LIKE '%/%')
  ),
  virtual_path TEXT,
  file_record TEXT NOT NULL,
  sort_order BIGINT NOT NULL CHECK(sort_order>=0),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(preview_session_id,role,logical_name),
  UNIQUE(preview_session_id,virtual_path),
  CHECK(
    (role IN ('PARENT','BIOS_BUNDLE') AND virtual_path IS NULL) OR
    (role IN ('PROJECT_FILE','RUNTIME_FILE') AND virtual_path IS NULL) OR
    (role IN ('EXTERNAL_FILE','DISC') AND virtual_path IS NOT NULL AND
      substr(virtual_path,1,1)='/' AND strpos(virtual_path,chr(92))=0 AND
      virtual_path NOT LIKE '%?%' AND virtual_path NOT LIKE '%#%' AND
      true AND virtual_path NOT LIKE '%//%' AND
      virtual_path NOT LIKE '%/./%' AND virtual_path NOT LIKE '%/../%' AND
      virtual_path NOT LIKE '%/.' AND virtual_path NOT LIKE '%/..')
  )
);

CREATE TABLE "runtime_preview_sessions" (
  id TEXT PRIMARY KEY,
  scope_id TEXT NOT NULL,
  content_revision TEXT NOT NULL,
  return_to TEXT NOT NULL,
  target_platform_instance_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  actor_user_id TEXT NOT NULL ,
  idempotency_key TEXT NOT NULL,
  title TEXT NOT NULL CHECK(octet_length(title) BETWEEN 1 AND 800),
  content_kind TEXT NOT NULL ,
  content_file_record TEXT NOT NULL,
  content_logical_name TEXT NOT NULL CHECK(octet_length(content_logical_name) BETWEEN 1 AND 512),
  content_format TEXT NOT NULL CHECK(
    length(content_format) BETWEEN 2 AND 64 AND content_format=upper(content_format)
    AND content_format !~ '^.*[^A-Z0-9_].*$'
  ),
  dependency_snapshot_json TEXT NOT NULL,
  default_dos_entry TEXT,
  checkpoint_payload_file_record TEXT,
  checkpoint_format TEXT CHECK(length(checkpoint_format) BETWEEN 1 AND 128),
  checkpoint_created_at_ms BIGINT CHECK(checkpoint_created_at_ms>=0),
  restore_from_preview_id TEXT,
  restore_payload_file_record TEXT,
  restore_checkpoint_format TEXT CHECK(length(restore_checkpoint_format) BETWEEN 1 AND 128),
  emulator_game_id BIGINT CHECK(emulator_game_id IS NULL OR emulator_game_id>0),
  credential_sha256 BYTEA NOT NULL CHECK(length(credential_sha256)=32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','FINISHED','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms BIGINT NOT NULL CHECK(bootstrap_expires_at_ms>=0),
  hard_expires_at_ms BIGINT NOT NULL CHECK(hard_expires_at_ms>=bootstrap_expires_at_ms),
  activated_at_ms BIGINT,
  finished_at_ms BIGINT,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=0),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  UNIQUE(actor_user_id,idempotency_key),
  CHECK(state!='ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED'))=(finished_at_ms IS NOT NULL)),
  CHECK((checkpoint_payload_file_record IS NULL)=(checkpoint_format IS NULL)),
  CHECK((checkpoint_payload_file_record IS NULL)=(checkpoint_created_at_ms IS NULL)),
  CHECK((restore_payload_file_record IS NULL)=(restore_checkpoint_format IS NULL)),
  CHECK(restore_payload_file_record IS NULL OR restore_from_preview_id IS NOT NULL)
);

CREATE TABLE "launch_content_files" (
  launch_session_id TEXT NOT NULL ,
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 512),
  file_record TEXT NOT NULL,
  format_version TEXT NOT NULL CHECK(
    length(format_version) BETWEEN 2 AND 64 AND format_version=upper(format_version)
    AND format_version !~ '^.*[^A-Z0-9_].*$'
  ),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(launch_session_id,logical_name)
);

CREATE TABLE isolated_runtime_bootstrap_tickets (
  ticket_sha256 BYTEA PRIMARY KEY CHECK(length(ticket_sha256)=32),
  launch_id TEXT UNIQUE ,
  preview_id TEXT UNIQUE ,
  profile_id TEXT NOT NULL ,
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  expires_at_ms BIGINT NOT NULL CHECK(expires_at_ms>=0),
  consumed_at_ms BIGINT CHECK(consumed_at_ms IS NULL OR consumed_at_ms BETWEEN 0 AND expires_at_ms),
  CHECK((launch_id IS NULL) <> (preview_id IS NULL))
);

CREATE TABLE isolated_runtime_capabilities (
  credential_sha256 BYTEA PRIMARY KEY CHECK(length(credential_sha256)=32),
  launch_id TEXT UNIQUE ,
  preview_id TEXT UNIQUE ,
  profile_id TEXT NOT NULL ,
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  issued_at_ms BIGINT NOT NULL CHECK(issued_at_ms>=0),
  expires_at_ms BIGINT NOT NULL CHECK(expires_at_ms>=issued_at_ms),
  revoked_at_ms BIGINT CHECK(revoked_at_ms IS NULL OR revoked_at_ms>=issued_at_ms),
  CHECK((launch_id IS NULL) <> (preview_id IS NULL))
);

CREATE TABLE "launch_sessions" (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL ,
  game_id TEXT NOT NULL ,
  core_id TEXT NOT NULL ,
  provider_id TEXT NOT NULL ,
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  content_kind TEXT NOT NULL ,
  dependency_snapshot_json TEXT NOT NULL CHECK((dependency_snapshot_json IS JSON)),
  compatibility_code TEXT NOT NULL,
  save_state_id TEXT ,
  dos_entry_path TEXT,
  return_to TEXT NOT NULL,
  credential_sha256 BYTEA NOT NULL CHECK(length(credential_sha256) = 32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','FINISHED','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms BIGINT NOT NULL,
  activated_at_ms BIGINT,
  finished_at_ms BIGINT,
  hard_expires_at_ms BIGINT NOT NULL,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  initial_disc_index BIGINT NOT NULL DEFAULT 0 CHECK(initial_disc_index BETWEEN 0 AND 7),
  CHECK(hard_expires_at_ms >= bootstrap_expires_at_ms),
  CHECK(state != 'ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED')) = (finished_at_ms IS NOT NULL))
);

CREATE TABLE "launch_external_files" (
  launch_session_id TEXT NOT NULL ,
  virtual_path TEXT NOT NULL CHECK(length(virtual_path) BETWEEN 1 AND 512),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 255),
  file_record TEXT NOT NULL,
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms >= 0),
  kind TEXT NOT NULL DEFAULT 'BIOS' CHECK(kind IN ('BIOS','BIOS_BUNDLE','PARENT','DISC')),
  PRIMARY KEY(launch_session_id, virtual_path),
  UNIQUE(launch_session_id, logical_name),
  CHECK(substr(virtual_path,1,1)='/' AND
        strpos(virtual_path,chr(92))=0 AND
        virtual_path NOT LIKE '%?%' AND
        virtual_path NOT LIKE '%#%' AND
        true AND
        virtual_path NOT LIKE '%//%' AND
        virtual_path NOT LIKE '%/./%' AND
        virtual_path NOT LIKE '%/../%' AND
        virtual_path NOT LIKE '%/.' AND
        virtual_path NOT LIKE '%/..'),
  CHECK(logical_name NOT LIKE '%/%' AND
        strpos(logical_name,chr(92))=0 AND
        logical_name NOT IN ('','.','..') AND
        true)
);

CREATE TABLE "save_states" (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL ,
  game_id TEXT NOT NULL ,
  checkpoint_format TEXT NOT NULL CHECK(length(checkpoint_format) BETWEEN 1 AND 128),
  payload_file_record TEXT NOT NULL,
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256=lower(payload_sha256)),
  payload_size_bytes BIGINT NOT NULL CHECK(payload_size_bytes BETWEEN 1 AND 268435456),
  screenshot_file_record TEXT,
  name TEXT NOT NULL,
  active_duration_ms BIGINT NOT NULL CHECK(active_duration_ms >= 0),
  dos_entry_path TEXT,
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  deleted_at_ms BIGINT,
  source_launch_session_id TEXT NOT NULL ,
  disc_index BIGINT CHECK(disc_index BETWEEN 0 AND 7)
);

CREATE TABLE "play_sessions" (
  id TEXT PRIMARY KEY,
  launch_session_id TEXT NOT NULL UNIQUE ,
  profile_id TEXT NOT NULL ,
  game_id TEXT NOT NULL ,
  started_at_ms BIGINT NOT NULL,
  last_reported_at_ms BIGINT NOT NULL,
  ended_at_ms BIGINT,
  active_duration_ms BIGINT NOT NULL DEFAULT 0 CHECK(active_duration_ms >= 0),
  state TEXT NOT NULL CHECK(state IN ('ACTIVE','FINISHED','ABANDONED')),
  version BIGINT NOT NULL DEFAULT 1,
  created_at_ms BIGINT NOT NULL,
  updated_at_ms BIGINT NOT NULL,
  CHECK((state = 'ACTIVE') = (ended_at_ms IS NULL))
);

CREATE TABLE import_batch_discards (
  kind TEXT NOT NULL CHECK(kind IN ('IMPORT','SOURCE')),
  import_id TEXT NOT NULL,
  requested_by_user_id TEXT NOT NULL ,
  state TEXT NOT NULL CHECK(state IN ('REQUESTED','COMPLETED','FAILED')),
  error_code TEXT,
  requested_at_ms BIGINT NOT NULL CHECK(requested_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=requested_at_ms),
  completed_at_ms BIGINT,
  PRIMARY KEY(kind,import_id),
  CHECK((state='COMPLETED')=(completed_at_ms IS NOT NULL)),
  CHECK((state='FAILED')=(error_code IS NOT NULL))
);

CREATE TABLE server_import_upload_owners (
  upload_session_id TEXT PRIMARY KEY ,
  kind TEXT NOT NULL CHECK(kind IN ('SOURCE')),
  source_item_id TEXT NOT NULL,
  UNIQUE(kind,source_item_id)
);

CREATE TABLE game_save_versions (
  save_state_id TEXT PRIMARY KEY ,
  data_version BIGINT NOT NULL DEFAULT 1 CHECK(data_version>=1),
  last_synced_at_ms BIGINT CHECK(last_synced_at_ms>=0),
  last_writer_launch_session_id TEXT 
);

CREATE TABLE launch_game_save_bindings (
  launch_session_id TEXT PRIMARY KEY ,
  save_state_id TEXT ,
  initial_active_duration_ms BIGINT NOT NULL DEFAULT 0 CHECK(initial_active_duration_ms>=0),
  expected_data_version BIGINT NOT NULL DEFAULT 0 CHECK(expected_data_version>=0)
);

CREATE TABLE launch_payload_retirements (
  launch_session_id TEXT PRIMARY KEY ,
  due_at_ms BIGINT NOT NULL CHECK(due_at_ms>=0),
  released_at_ms BIGINT CHECK(released_at_ms IS NULL OR released_at_ms>=0)
);

CREATE TABLE metadata_media_runs (
  scrape_run_id TEXT PRIMARY KEY ,
  order_frozen_at_ms BIGINT CHECK(order_frozen_at_ms IS NULL OR order_frozen_at_ms>=0),
  charged_bytes BIGINT NOT NULL DEFAULT 0 CHECK(charged_bytes BETWEEN 0 AND 104857600),
  version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms BIGINT NOT NULL CHECK(updated_at_ms>=created_at_ms)
);

CREATE TABLE runtime_sessions (
  id TEXT PRIMARY KEY,
  auth_session_id TEXT NOT NULL UNIQUE ,
  token_sha256 BYTEA NOT NULL UNIQUE CHECK(length(token_sha256)=32),
  created_at_ms BIGINT NOT NULL CHECK(created_at_ms>=0),
  renewed_at_ms BIGINT NOT NULL CHECK(renewed_at_ms>=created_at_ms),
  expires_at_ms BIGINT NOT NULL CHECK(expires_at_ms=renewed_at_ms+86400000)
);

CREATE TABLE profile_game_activity (
  profile_id TEXT NOT NULL ,
  game_id TEXT NOT NULL ,
  last_played_at_ms BIGINT NOT NULL CHECK(last_played_at_ms>=0),
  active_duration_ms BIGINT NOT NULL CHECK(active_duration_ms>=0),
  session_count BIGINT NOT NULL CHECK(session_count>0),
  PRIMARY KEY(profile_id,game_id)
);

ALTER TABLE "users" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "user_credentials" ADD FOREIGN KEY ("user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "auth_sessions" ADD FOREIGN KEY ("user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "account_links" ADD FOREIGN KEY ("target_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "account_links" ADD FOREIGN KEY ("created_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "account_links" ADD FOREIGN KEY ("consumed_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "account_links" ADD FOREIGN KEY ("revoked_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "instance_state" ADD FOREIGN KEY ("initial_admin_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "platform_cores" ADD FOREIGN KEY ("platform_id") REFERENCES platforms(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "platform_cores" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_target_bindings" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_target_bindings" ADD FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_binding_platforms" ADD FOREIGN KEY ("binding_id") REFERENCES runtime_target_bindings(binding_id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_binding_platforms" ADD FOREIGN KEY(platform_id,core_id) REFERENCES platform_cores(platform_id,core_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_binding_content_kinds" ADD FOREIGN KEY ("binding_id") REFERENCES runtime_target_bindings(binding_id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_binding_content_kinds" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "platform_instances" ADD FOREIGN KEY(platform_id, default_core_id) REFERENCES platform_cores(platform_id, core_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_targets" ADD FOREIGN KEY ("provider_id") REFERENCES runtime_providers(provider_id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "job_events" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "job_input_snapshots" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "audit_events" ADD FOREIGN KEY ("actor_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "upload_sessions" ADD FOREIGN KEY ("finalize_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "upload_files" ADD FOREIGN KEY ("upload_session_id") REFERENCES upload_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_files" ADD FOREIGN KEY ("id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_files" ADD FOREIGN KEY ("upload_session_id") REFERENCES upload_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "upload_parts" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "upload_consumptions" ADD FOREIGN KEY ("upload_session_id") REFERENCES upload_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "upload_consumptions" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "bios_installations" ADD FOREIGN KEY ("requirement_id") REFERENCES bios_requirements(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "bios_installations" ADD FOREIGN KEY ("server_import_candidate_id") REFERENCES server_bios_import_candidates(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_machines" ADD FOREIGN KEY ("dat_version_id") REFERENCES dat_versions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_rom_entries" ADD FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_rom_entries" ADD FOREIGN KEY(dat_version_id, machine_name, bios_name) REFERENCES dat_bios_sets(dat_version_id, machine_name, bios_name) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_disk_entries" ADD FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_bios_sets" ADD FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_imports" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_imports" ADD FOREIGN KEY ("created_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_candidates" ADD FOREIGN KEY(server_import_id,requirement_id) REFERENCES server_bios_import_items(server_import_id,requirement_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "bios_requirements" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "bios_requirements" ADD FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_versions" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dat_versions" ADD FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_items" ADD FOREIGN KEY ("server_import_id") REFERENCES server_imports(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_items" ADD FOREIGN KEY ("requirement_id") REFERENCES bios_requirements(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_items" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_items" ADD FOREIGN KEY ("active_installation_id_snapshot") REFERENCES bios_installations(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_items" ADD FOREIGN KEY ("previous_installation_id") REFERENCES bios_installations(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_bios_import_items" ADD FOREIGN KEY ("new_installation_id") REFERENCES bios_installations(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_job_files" ADD FOREIGN KEY ("import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_job_files" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_job_file_resolutions" ADD FOREIGN KEY ("import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_job_file_resolutions" ADD FOREIGN KEY ("replacement_import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_job_file_resolutions" ADD FOREIGN KEY ("actor_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_job_file_resolutions" ADD FOREIGN KEY(import_job_id,upload_file_id) REFERENCES import_job_files(import_job_id,upload_file_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("payload_release_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("target_platform_instance_id") REFERENCES platform_instances(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("selected_candidate_id") REFERENCES scrape_candidates(id) ON DELETE SET NULL DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("cover_candidate_asset_id") REFERENCES scrape_candidate_assets(id) ON DELETE SET NULL DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("background_candidate_asset_id") REFERENCES scrape_candidate_assets(id) ON DELETE SET NULL DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("cover_uploaded_asset_id") REFERENCES review_uploaded_assets(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("video_uploaded_asset_id") REFERENCES review_uploaded_assets(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_items" ADD FOREIGN KEY ("effective_source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_assets" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_source_files" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_source_files" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_source_snapshots" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_source_snapshots" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_source_snapshot_files" ADD FOREIGN KEY ("source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_source_snapshot_files" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_runtime_files" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_dos_entries" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_multidisc_entries" ADD FOREIGN KEY ("source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_multidisc_entries" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_uploaded_assets" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_uploaded_assets" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("review_draft_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("requested_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("base_source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("result_source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("upload_session_id") REFERENCES upload_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_multidisc_attachments" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_draft_screenshot_assets" ADD FOREIGN KEY ("review_draft_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_draft_screenshot_assets" ADD FOREIGN KEY ("candidate_asset_id") REFERENCES scrape_candidate_assets(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_bulk_approvals" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_bulk_approvals" ADD FOREIGN KEY ("max_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_bulk_approvals" ADD FOREIGN KEY ("created_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_draft_tags" ADD FOREIGN KEY ("review_draft_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_draft_tags" ADD FOREIGN KEY ("tag_id") REFERENCES tags(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_draft_tags" ADD FOREIGN KEY ("assigned_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_provider_cache" ADD FOREIGN KEY ("current_response_id") REFERENCES metadata_provider_responses(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_scrape_query_attempts" ADD FOREIGN KEY ("scrape_run_id") REFERENCES metadata_scrape_runs(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_scrape_query_attempts" ADD FOREIGN KEY ("content_hash_evidence_id") REFERENCES content_hash_evidence(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_scrape_query_attempts" ADD FOREIGN KEY ("provider_response_id") REFERENCES metadata_provider_responses(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidates" ADD FOREIGN KEY ("scrape_run_id") REFERENCES metadata_scrape_runs(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidates" ADD FOREIGN KEY ("primary_response_id") REFERENCES metadata_provider_responses(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidate_hits" ADD FOREIGN KEY ("scrape_candidate_id") REFERENCES scrape_candidates(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidate_hits" ADD FOREIGN KEY ("query_attempt_id") REFERENCES metadata_scrape_query_attempts(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidate_assets" ADD FOREIGN KEY ("scrape_candidate_id") REFERENCES scrape_candidates(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidate_assets" ADD FOREIGN KEY ("provider_response_id") REFERENCES metadata_provider_responses(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "scrape_candidate_assets" ADD FOREIGN KEY ("media_fetch_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "content_hash_evidence" ADD FOREIGN KEY ("scrape_run_id") REFERENCES metadata_scrape_runs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "content_identity_claims" ADD FOREIGN KEY ("platform_id") REFERENCES platforms(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_group_requests" ADD FOREIGN KEY ("import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_group_requests" ADD FOREIGN KEY ("actor_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_jobs" ADD FOREIGN KEY ("upload_session_id") REFERENCES upload_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_jobs" ADD FOREIGN KEY ("target_platform_instance_id") REFERENCES platform_instances(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_jobs" ADD FOREIGN KEY ("platform_id") REFERENCES platforms(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_jobs" ADD FOREIGN KEY ("default_core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_jobs" ADD FOREIGN KEY ("payload_release_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_jobs" ADD FOREIGN KEY ("reconfigured_from_import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_duplicate_matches" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_item_duplicate_matches" ADD FOREIGN KEY ("existing_game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("review_draft_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("base_source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("result_source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("provider_id") REFERENCES runtime_providers(provider_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("dat_version_id") REFERENCES dat_versions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("upload_file_id") REFERENCES upload_files(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_arcade_parent_attachments" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_preview_bindings" ADD FOREIGN KEY ("preview_session_id") REFERENCES runtime_preview_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_preview_bindings" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_preview_bindings" ADD FOREIGN KEY ("source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_runtime_screenshots" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_runtime_screenshots" ADD FOREIGN KEY ("preview_session_id") REFERENCES runtime_preview_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "review_runtime_screenshots" ADD FOREIGN KEY ("source_snapshot_id") REFERENCES import_item_source_snapshots(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_scrape_runs" ADD FOREIGN KEY ("import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_scrape_runs" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_scrape_runs" ADD FOREIGN KEY ("job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "dos_entries" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "tags" ADD FOREIGN KEY ("created_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "tags" ADD FOREIGN KEY ("updated_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_tags" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_tags" ADD FOREIGN KEY ("tag_id") REFERENCES tags(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_tags" ADD FOREIGN KEY ("assigned_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "favorite_games" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "favorite_games" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "favorite_folders" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "favorite_folder_games" ADD FOREIGN KEY(profile_id,folder_id) REFERENCES favorite_folders(profile_id,id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "favorite_folder_games" ADD FOREIGN KEY(profile_id,game_id) REFERENCES favorite_games(profile_id,game_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_assets" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_variants" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_variants" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_variants" ADD FOREIGN KEY ("provider_id") REFERENCES runtime_providers(provider_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_variants" ADD FOREIGN KEY ("dat_version_id") REFERENCES dat_versions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_variants" ADD FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "games" ADD FOREIGN KEY ("platform_instance_id") REFERENCES platform_instances(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "games" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "games" ADD FOREIGN KEY ("payload_release_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "variant_dependencies" ADD FOREIGN KEY ("game_variant_id") REFERENCES game_variants(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "variant_dependencies" ADD FOREIGN KEY ("dat_version_id") REFERENCES dat_versions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "variant_files" ADD FOREIGN KEY ("game_variant_id") REFERENCES game_variants(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_files" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_imports" ADD FOREIGN KEY ("scan_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_imports" ADD FOREIGN KEY ("import_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_imports" ADD FOREIGN KEY ("created_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_metadata_files" ADD FOREIGN KEY ("import_id") REFERENCES source_imports(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_collection_tags" ADD FOREIGN KEY ("collection_id") REFERENCES source_import_collections(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_collection_tags" ADD FOREIGN KEY ("tag_id") REFERENCES tags(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_collection_tags" ADD FOREIGN KEY ("assigned_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_item_files" ADD FOREIGN KEY ("item_id") REFERENCES source_import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_item_assets" ADD FOREIGN KEY ("item_id") REFERENCES source_import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_collections" ADD FOREIGN KEY ("import_id") REFERENCES source_imports(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_collections" ADD FOREIGN KEY ("target_platform_instance_id") REFERENCES platform_instances(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_collections" ADD FOREIGN KEY ("target_platform_id") REFERENCES platforms(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_collections" ADD FOREIGN KEY ("target_default_core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("import_id") REFERENCES source_imports(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("collection_id") REFERENCES source_import_collections(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("payload_release_job_id") REFERENCES jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("library_import_job_id") REFERENCES import_jobs(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("library_import_item_id") REFERENCES import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("published_game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_items" ADD FOREIGN KEY ("existing_game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_item_companions" ADD FOREIGN KEY ("item_id") REFERENCES source_import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "source_import_item_companions" ADD FOREIGN KEY ("candidate_item_id") REFERENCES source_import_items(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_preview_files" ADD FOREIGN KEY ("preview_session_id") REFERENCES runtime_preview_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_preview_sessions" ADD FOREIGN KEY ("target_platform_instance_id") REFERENCES platform_instances(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_preview_sessions" ADD FOREIGN KEY ("actor_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_preview_sessions" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_content_files" ADD FOREIGN KEY ("launch_session_id") REFERENCES launch_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "isolated_runtime_bootstrap_tickets" ADD FOREIGN KEY ("launch_id") REFERENCES launch_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "isolated_runtime_bootstrap_tickets" ADD FOREIGN KEY ("preview_id") REFERENCES runtime_preview_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "isolated_runtime_bootstrap_tickets" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "isolated_runtime_capabilities" ADD FOREIGN KEY ("launch_id") REFERENCES launch_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "isolated_runtime_capabilities" ADD FOREIGN KEY ("preview_id") REFERENCES runtime_preview_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "isolated_runtime_capabilities" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY ("core_id") REFERENCES cores(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY ("provider_id") REFERENCES runtime_providers(provider_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY ("content_kind") REFERENCES content_kinds(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY ("save_state_id") REFERENCES save_states(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_sessions" ADD FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_external_files" ADD FOREIGN KEY ("launch_session_id") REFERENCES launch_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "save_states" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "save_states" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "save_states" ADD FOREIGN KEY ("source_launch_session_id") REFERENCES launch_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "play_sessions" ADD FOREIGN KEY ("launch_session_id") REFERENCES launch_sessions(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "play_sessions" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "play_sessions" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "import_batch_discards" ADD FOREIGN KEY ("requested_by_user_id") REFERENCES users(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "server_import_upload_owners" ADD FOREIGN KEY ("upload_session_id") REFERENCES upload_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_save_versions" ADD FOREIGN KEY ("save_state_id") REFERENCES save_states(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "game_save_versions" ADD FOREIGN KEY ("last_writer_launch_session_id") REFERENCES launch_sessions(id) ON DELETE SET NULL DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_game_save_bindings" ADD FOREIGN KEY ("launch_session_id") REFERENCES launch_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_game_save_bindings" ADD FOREIGN KEY ("save_state_id") REFERENCES save_states(id) ON DELETE SET NULL DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "launch_payload_retirements" ADD FOREIGN KEY ("launch_session_id") REFERENCES launch_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "metadata_media_runs" ADD FOREIGN KEY ("scrape_run_id") REFERENCES metadata_scrape_runs(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "runtime_sessions" ADD FOREIGN KEY ("auth_session_id") REFERENCES auth_sessions(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "profile_game_activity" ADD FOREIGN KEY ("profile_id") REFERENCES profiles(id) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE "profile_game_activity" ADD FOREIGN KEY ("game_id") REFERENCES games(id) DEFERRABLE INITIALLY IMMEDIATE;

CREATE INDEX archive_entries_path ON archive_entries(
  (CASE WHEN (archive_file_record IS JSON) THEN ((archive_file_record)::jsonb #>> '{path}') END)
);

CREATE INDEX import_item_assets_blob ON import_item_assets(file_record);

CREATE UNIQUE INDEX import_item_initial_source ON import_item_source_snapshots(import_item_id)
WHERE created_by='IDENTIFICATION';

CREATE INDEX game_files_game ON game_files(game_id,sort_order,logical_name);

CREATE INDEX source_import_item_companions_blob ON source_import_item_companions(file_record);

CREATE INDEX account_links_creator ON account_links(created_by_user_id,kind,created_at_ms DESC);

CREATE INDEX account_links_kind_created ON account_links(kind,created_at_ms DESC,id DESC);

CREATE INDEX account_links_target ON account_links(target_user_id,kind,created_at_ms DESC);

CREATE INDEX audit_events_actor ON audit_events(actor_user_id,created_at_ms,id);

CREATE INDEX audit_events_resource ON audit_events(resource_type,resource_id,created_at_ms,id);

CREATE INDEX auth_sessions_expiry ON auth_sessions(absolute_expires_at_ms,revoked_at_ms);

CREATE INDEX auth_sessions_user_active ON auth_sessions(user_id,revoked_at_ms,absolute_expires_at_ms);

CREATE UNIQUE INDEX bios_installations_active ON bios_installations(requirement_id) WHERE is_active = 1;

CREATE UNIQUE INDEX bios_installations_server_candidate ON bios_installations(server_import_candidate_id)
WHERE server_import_candidate_id IS NOT NULL;

CREATE UNIQUE INDEX dat_bios_sets_one_default ON dat_bios_sets(dat_version_id, machine_name) WHERE is_default = 1;

CREATE INDEX dat_rom_entries_crc32 ON dat_rom_entries(dat_version_id, crc32) WHERE crc32 IS NOT NULL;

CREATE INDEX dat_rom_entries_sha1 ON dat_rom_entries(dat_version_id, sha1) WHERE sha1 IS NOT NULL;

CREATE UNIQUE INDEX dat_versions_active
ON dat_versions(provider_id,target_id) WHERE is_active=1;

CREATE UNIQUE INDEX dat_versions_bytes
ON dat_versions(provider_id,target_id,sha256,parser_version);

CREATE INDEX favorite_folder_games_folder
ON favorite_folder_games(profile_id,folder_id,created_at_ms,game_id);

CREATE INDEX favorite_folder_games_game
ON favorite_folder_games(profile_id,game_id,folder_id);

CREATE INDEX favorite_folders_profile_created
ON favorite_folders(profile_id,created_at_ms,id);

CREATE INDEX favorite_games_game
ON favorite_games(game_id,profile_id);

CREATE INDEX favorite_games_profile_created
ON favorite_games(profile_id,created_at_ms DESC,game_id DESC);

CREATE INDEX fk_bios_installations_blob ON bios_installations(file_record);

CREATE INDEX fk_import_item_duplicate_matches_game
ON import_item_duplicate_matches(existing_game_id);

CREATE INDEX fk_import_item_multidisc_blob ON import_item_multidisc_entries(file_record);

CREATE INDEX fk_import_item_multidisc_upload ON import_item_multidisc_entries(upload_file_id);

CREATE INDEX fk_import_items_job ON import_items(import_job_id);

CREATE INDEX fk_import_job_file_resolutions_replacement
ON import_job_file_resolutions(replacement_import_job_id);

CREATE INDEX fk_import_jobs_platform ON import_jobs(target_platform_instance_id);

CREATE INDEX fk_import_jobs_reconfigured_from
ON import_jobs(reconfigured_from_import_job_id);

CREATE INDEX fk_launch_external_files_blob ON launch_external_files(file_record);

CREATE INDEX fk_launch_game ON launch_sessions(game_id);

CREATE INDEX fk_platform_instances_default_core ON platform_instances(default_core_id);

CREATE INDEX fk_upload_files_session ON upload_files(upload_session_id);

CREATE INDEX game_tags_tag ON game_tags(tag_id,game_id);

CREATE INDEX game_variants_game ON game_variants(game_id, core_id);

CREATE INDEX games_library ON games(status, platform_instance_id, search_text, id);

CREATE INDEX import_group_requests_actor ON import_group_requests(actor_user_id);

CREATE INDEX import_items_queue ON import_items(state, updated_at_ms, id);

CREATE INDEX import_job_file_resolutions_actor
ON import_job_file_resolutions(actor_user_id,created_at_ms);

CREATE UNIQUE INDEX isolated_runtime_capability_origin
ON isolated_runtime_capabilities(expected_origin);

CREATE UNIQUE INDEX isolated_runtime_ticket_origin
ON isolated_runtime_bootstrap_tickets(expected_origin);

CREATE INDEX job_events_job ON job_events(job_id,id);

CREATE INDEX job_events_scope ON job_events(scope_type,scope_id,id);

CREATE INDEX jobs_claim ON jobs(state,available_at_ms);

CREATE INDEX jobs_recovery ON jobs(kind,state,leased_until_ms,execution_deadline_at_ms);

CREATE INDEX jobs_scope ON jobs(scope_type,scope_id);

CREATE INDEX source_collection_tags_tag ON source_collection_tags(tag_id,collection_id);

CREATE INDEX source_collections_mapping ON source_import_collections(import_id,mapping_action,id);

CREATE INDEX source_collections_page ON source_import_collections(import_id,metadata_relative_path,segment_ordinal,id);

CREATE INDEX source_imports_history ON source_imports(created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX source_imports_one_active_execution ON source_imports((1))
WHERE import_job_id IS NOT NULL AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

CREATE INDEX source_imports_state ON source_imports(state,updated_at_ms DESC,id DESC);

CREATE INDEX source_items_collection ON source_import_items(import_id,collection_id,title,id);

CREATE UNIQUE INDEX source_items_library_review ON source_import_items(library_import_item_id)
WHERE library_import_item_id IS NOT NULL;

CREATE INDEX source_items_outcome ON source_import_items(import_id,execution_state,title,id);

CREATE INDEX source_items_page ON source_import_items(import_id,title,id);

CREATE INDEX source_metadata_page ON source_import_metadata_files(import_id,relative_path);

CREATE UNIQUE INDEX platform_instances_catalog_template_key_unique
ON platform_instances(catalog_template_key)
WHERE catalog_template_key IS NOT NULL;

CREATE UNIQUE INDEX review_arcade_parent_active
ON review_arcade_parent_attachments(import_item_id)
WHERE state='PENDING';

CREATE INDEX review_arcade_parent_history
ON review_arcade_parent_attachments(import_item_id,created_at_ms,id);

CREATE INDEX review_bulk_approvals_history ON review_bulk_approvals(created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX review_bulk_approvals_one_active ON review_bulk_approvals((1))
WHERE state IN ('QUEUED','RUNNING');

CREATE INDEX review_draft_tags_tag ON review_draft_tags(tag_id,review_draft_id);

CREATE UNIQUE INDEX review_multidisc_attachment_active
ON review_multidisc_attachments(import_item_id) WHERE state='PENDING';

CREATE INDEX review_multidisc_attachment_actor
ON review_multidisc_attachments(requested_by_user_id,created_at_ms,id);

CREATE INDEX review_multidisc_attachment_history
ON review_multidisc_attachments(import_item_id,created_at_ms,id);

CREATE INDEX runtime_preview_files_blob ON runtime_preview_files(file_record);

CREATE INDEX runtime_preview_sessions_actor ON runtime_preview_sessions(actor_user_id);

CREATE INDEX review_preview_bindings_item
ON review_preview_bindings(import_item_id,preview_session_id);

CREATE INDEX review_preview_bindings_source ON review_preview_bindings(source_snapshot_id);

CREATE INDEX runtime_preview_sessions_target ON runtime_preview_sessions(target_platform_instance_id);

CREATE INDEX review_queue ON import_items(review_updated_at_ms, id) WHERE state='REVIEW_PENDING';

CREATE INDEX review_runtime_screenshots_blob ON review_runtime_screenshots(file_record);

CREATE INDEX review_runtime_screenshots_preview ON review_runtime_screenshots(preview_session_id);

CREATE INDEX review_runtime_screenshots_source ON review_runtime_screenshots(source_snapshot_id);

CREATE INDEX review_uploaded_assets_item ON review_uploaded_assets(import_item_id, created_at_ms, id);

CREATE INDEX save_states_library ON save_states(profile_id, game_id, created_at_ms DESC, id DESC);

CREATE INDEX save_states_payload ON save_states(payload_file_record);

CREATE INDEX save_states_source_launch
ON save_states(source_launch_session_id,created_at_ms DESC,id DESC)
WHERE deleted_at_ms IS NULL;

CREATE INDEX server_bios_candidates_page
ON server_bios_import_candidates(server_import_id,requirement_id,rank_ordinal,id);

CREATE UNIQUE INDEX server_bios_candidates_selected
ON server_bios_import_candidates(server_import_id,requirement_id) WHERE state='SELECTED';

CREATE INDEX server_bios_items_page ON server_bios_import_items(server_import_id,core_name_snapshot,logical_name,requirement_id);

CREATE INDEX server_imports_history ON server_imports(kind,created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX server_imports_one_active_kind ON server_imports(kind)
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

CREATE INDEX server_imports_state ON server_imports(state,updated_at_ms DESC,id DESC);

CREATE UNIQUE INDEX tags_active_name_key
ON tags(name_key) WHERE status='ACTIVE';

CREATE INDEX tags_active_page ON tags(status,name_key,id);

CREATE INDEX tags_updated_page ON tags(status,updated_at_ms DESC,id DESC);

CREATE UNIQUE INDEX upload_consumptions_whole_session
ON upload_consumptions(upload_session_id) WHERE upload_file_id IS NULL;

CREATE INDEX users_list_created ON users(created_at_ms DESC,id DESC);

CREATE INDEX users_list_last_login ON users(last_login_at_ms DESC,id DESC);

CREATE INDEX users_list_username ON users(username,id);

CREATE INDEX idx_archive_entries_content_digest
ON archive_entries(
 (CASE WHEN (archive_file_record IS JSON) THEN ((archive_file_record)::jsonb #>> '{sha256}') END),ordinal);

CREATE INDEX launch_game_save_target ON launch_game_save_bindings(save_state_id);

CREATE INDEX bios_installations_retirement ON bios_installations(updated_at_ms,id)
WHERE is_active=0 AND file_record IS NOT NULL;

CREATE INDEX variant_files_bios_blob ON variant_files(file_record,game_variant_id,logical_name)
WHERE role='BIOS_BUNDLE';

CREATE INDEX bios_installations_active_blob ON bios_installations(file_record) WHERE is_active=1;

CREATE INDEX launch_payload_retirement ON launch_payload_retirements(due_at_ms,launch_session_id)
WHERE released_at_ms IS NULL;

CREATE UNIQUE INDEX scrape_candidate_assets_media_job ON scrape_candidate_assets(media_fetch_job_id)
  WHERE media_fetch_job_id IS NOT NULL;

CREATE INDEX scrape_candidate_assets_media_order ON scrape_candidate_assets(scrape_candidate_id,media_fetch_order);

CREATE INDEX profile_game_activity_recent
ON profile_game_activity(profile_id,last_played_at_ms DESC,game_id DESC);

CREATE INDEX profile_game_activity_duration
ON profile_game_activity(profile_id,active_duration_ms DESC,last_played_at_ms DESC,game_id DESC);

CREATE INDEX profile_game_activity_sessions
ON profile_game_activity(profile_id,session_count DESC,last_played_at_ms DESC,game_id DESC);

CREATE INDEX play_sessions_profile_started
ON play_sessions(profile_id,started_at_ms DESC,id DESC);

CREATE INDEX play_sessions_profile_game
ON play_sessions(profile_id,game_id,started_at_ms DESC);

CREATE INDEX games_updated ON games(updated_at_ms DESC,title ASC,id ASC);

CREATE INDEX games_added ON games(created_at_ms DESC,title ASC,id ASC);

CREATE INDEX games_title ON games(title ASC,id ASC);

CREATE INDEX game_assets_primary ON game_assets(game_id,kind,ordinal,id);

CREATE INDEX games_latest ON games(status,created_at_ms DESC,id DESC);

CREATE INDEX profile_game_activity_game ON profile_game_activity(game_id,profile_id);

CREATE INDEX play_sessions_game_profile ON play_sessions(game_id,profile_id,started_at_ms DESC);

CREATE INDEX game_variants_provider_target_game
ON game_variants(provider_id,target_id,game_id);

INSERT INTO instance_state(id,state,bootstrap_kind,initial_admin_user_id,test_default_password_active,version,created_at_ms,updated_at_ms,initialized_at_ms) VALUES(1,'PENDING',NULL,NULL,0,1,0,0,NULL);
