-- Clean pre-release baseline: import_review.

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
  completed_at_ms INTEGER, resolved_rejected_file_count INTEGER NOT NULL DEFAULT 0
CHECK(resolved_rejected_file_count BETWEEN 0 AND rejected_file_count), reconfigured_from_import_job_id TEXT REFERENCES import_jobs(id), already_imported_item_count INTEGER NOT NULL DEFAULT 0
CHECK(already_imported_item_count BETWEEN 0 AND discarded_item_count), already_imported_file_count INTEGER NOT NULL DEFAULT 0
CHECK(already_imported_file_count >= 0),
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

CREATE TABLE "import_job_file_resolutions" (
  import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  upload_file_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('RECONFIGURED')),
  replacement_import_job_id TEXT NOT NULL REFERENCES import_jobs(id),
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT REFERENCES users(id),
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','offline-recovery','startup-test-bootstrap','restore-security-fence'
  )),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_job_id,upload_file_id),
  FOREIGN KEY(import_job_id,upload_file_id) REFERENCES import_job_files(import_job_id,upload_file_id),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
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

CREATE TABLE "import_item_source_files" (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_item_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE "import_item_source_snapshots" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  revision_no INTEGER NOT NULL CHECK(revision_no>=1),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE'
    CHECK(content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL
    CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  created_by TEXT NOT NULL CHECK(created_by IN ('IDENTIFICATION','ARCADE_PARENT_ATTACHMENT','MULTI_DISC_ATTACHMENT')),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(import_item_id,revision_no),
  UNIQUE(import_item_id,source_manifest_digest)
);

CREATE TABLE "import_item_source_snapshot_files" (
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT NOT NULL,
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(source_snapshot_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE "import_item_core_validations" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  platform_instance_version INTEGER NOT NULL CHECK(platform_instance_version>=1),
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  core_artifact_version INTEGER NOT NULL DEFAULT 1 CHECK(core_artifact_version>=1),
  prepublish_generation INTEGER NOT NULL DEFAULT 4 CHECK(prepublish_generation IN (3,4)),
  dat_version_id TEXT REFERENCES dat_versions(id),
  default_dos_entry TEXT,
  source_manifest_digest TEXT NOT NULL,
  prepublish_input_digest TEXT NOT NULL CHECK(length(prepublish_input_digest)=64),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  UNIQUE(import_item_id,prepublish_input_digest)
);

CREATE TABLE "import_item_validation_files" (
  import_item_core_validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(import_item_core_validation_id,role,logical_name)
);

CREATE TABLE import_item_duplicate_matches (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  existing_game_id TEXT NOT NULL REFERENCES games(id),
  existing_game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  detected_stage TEXT NOT NULL CHECK(detected_stage = 'IDENTIFICATION'),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_item_id, existing_game_id)
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

CREATE TABLE import_item_multidisc_entries (
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 7),
  source_reference TEXT NOT NULL,
  normalized_reference TEXT NOT NULL,
  canonical_name TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('PRESENT','MISSING')),
  upload_file_id TEXT REFERENCES upload_files(id),
  blob_id TEXT REFERENCES blobs(id),
  source_logical_name TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(source_snapshot_id,ordinal),
  UNIQUE(source_snapshot_id,normalized_reference),
  UNIQUE(source_snapshot_id,canonical_name),
  CHECK(length(CAST(source_reference AS BLOB)) BETWEEN 1 AND 255),
  CHECK(length(CAST(normalized_reference AS BLOB)) BETWEEN 1 AND 255),
  CHECK(canonical_name=printf('disc-%03d.chd',ordinal+1)),
  CHECK(
    state='PRESENT' AND upload_file_id IS NOT NULL AND blob_id IS NOT NULL AND source_logical_name IS NOT NULL OR
    state='MISSING' AND upload_file_id IS NULL AND blob_id IS NULL AND source_logical_name IS NULL
  )
);

CREATE TABLE review_drafts (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL UNIQUE REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  selected_validation_id TEXT REFERENCES import_item_core_validations(id),
  selected_candidate_id TEXT REFERENCES scrape_candidates(id),
  cover_candidate_asset_id TEXT REFERENCES scrape_candidate_assets(id),
  background_candidate_asset_id TEXT REFERENCES scrape_candidate_assets(id),
  default_dos_entry TEXT,
  metadata_json TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL
, cover_uploaded_asset_id TEXT REFERENCES review_uploaded_assets(id), effective_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id));

CREATE TABLE "review_events" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  event_type TEXT NOT NULL CHECK(event_type IN (
    'DRAFT_SAVED','TARGET_CHANGED','SCRAPE_REQUESTED','CANDIDATE_APPLIED','CANDIDATE_REMOVED',
    'PARENT_UPLOAD_REQUESTED','PARENT_ATTACHMENT_ACCEPTED','PARENT_ATTACHMENT_REJECTED',
    'DISC_UPLOAD_REQUESTED','DISC_ATTACHMENT_ACCEPTED','DISC_ATTACHMENT_REJECTED','APPROVED','DISCARDED'
  )),
  actor_kind TEXT NOT NULL CHECK(actor_kind IN ('USER','SYSTEM')),
  actor_user_id TEXT REFERENCES users(id),
  actor_label TEXT CHECK(actor_label IN (
    'release-setup','offline-recovery','startup-test-bootstrap','restore-security-fence'
  )),
  before_json TEXT NOT NULL,
  after_json TEXT NOT NULL,
  diff_json TEXT NOT NULL,
  config_evidence_json TEXT NOT NULL,
  dat_evidence_json TEXT NOT NULL,
  provider_evidence_json TEXT NOT NULL,
  reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  CHECK(
    actor_kind='USER' AND actor_user_id IS NOT NULL AND actor_label IS NULL OR
    actor_kind='SYSTEM' AND actor_user_id IS NULL AND actor_label IS NOT NULL
  )
);

CREATE TABLE review_uploaded_assets (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  upload_file_id TEXT NOT NULL UNIQUE REFERENCES upload_files(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  kind TEXT NOT NULL CHECK(kind = 'COVER'),
  width_px INTEGER NOT NULL CHECK(width_px > 0),
  height_px INTEGER NOT NULL CHECK(height_px > 0),
  media_type TEXT NOT NULL CHECK(media_type IN ('image/png','image/jpeg','image/webp')),
  created_at_ms INTEGER NOT NULL
);

CREATE TABLE review_arcade_parent_attachments (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  base_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  result_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  dependency_machine TEXT NOT NULL CHECK(length(CAST(dependency_machine AS BLOB)) BETWEEN 1 AND 255),
  expected_logical_name TEXT NOT NULL,
  required_by_machine TEXT NOT NULL CHECK(length(CAST(required_by_machine AS BLOB)) BETWEEN 1 AND 255),
  depth INTEGER NOT NULL CHECK(depth BETWEEN 1 AND 63),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  dat_version_id TEXT NOT NULL REFERENCES dat_versions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  accepted_blob_id TEXT REFERENCES blobs(id),
  original_filename TEXT NOT NULL CHECK(length(CAST(original_filename AS BLOB)) BETWEEN 1 AND 255),
  observed_size_bytes INTEGER CHECK(observed_size_bytes IS NULL OR observed_size_bytes >= 0),
  observed_sha256 TEXT CHECK(observed_sha256 IS NULL OR (length(observed_sha256)=64 AND observed_sha256=lower(observed_sha256))),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(length(CAST(diagnostics_json AS BLOB)) <= 65536),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  finished_at_ms INTEGER,
  CHECK(expected_logical_name=dependency_machine||'.zip'),
  CHECK((state='ACCEPTED')=(accepted_blob_id IS NOT NULL AND result_source_snapshot_id IS NOT NULL)),
  CHECK((state IN ('REJECTED','FAILED_RETRYABLE','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK(state IN ('QUEUED','RUNNING') OR upload_file_id IS NULL OR observed_size_bytes IS NOT NULL)
);

CREATE TABLE review_multidisc_attachments (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  requested_by_user_id TEXT NOT NULL REFERENCES users(id),
  base_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  result_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  result_validation_id TEXT REFERENCES import_item_core_validations(id),
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  expected_set_digest TEXT NOT NULL
    CHECK(length(expected_set_digest)=64 AND expected_set_digest=lower(expected_set_digest)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(length(CAST(diagnostics_json AS BLOB))<=65536),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  finished_at_ms INTEGER,
  CHECK(
    state='ACCEPTED' AND result_source_snapshot_id IS NOT NULL AND result_validation_id IS NOT NULL OR
    state<>'ACCEPTED' AND result_source_snapshot_id IS NULL AND result_validation_id IS NULL
  ),
  CHECK((state IN ('REJECTED','FAILED_RETRYABLE','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED'))=(finished_at_ms IS NOT NULL))
);

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

CREATE TABLE review_draft_screenshot_assets (
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  candidate_asset_id TEXT NOT NULL REFERENCES scrape_candidate_assets(id),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(review_draft_id, ordinal),
  UNIQUE(review_draft_id, candidate_asset_id)
);

CREATE TABLE review_bulk_approvals (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  state TEXT NOT NULL CHECK(state IN (
    'QUEUED','RUNNING','CANCEL_REQUESTED','COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED'
  )),
  scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),
  scope_digest TEXT NOT NULL CHECK(length(scope_digest)=64 AND scope_digest=lower(scope_digest)),
  candidate_manifest_digest TEXT NOT NULL CHECK(
    length(candidate_manifest_digest)=64 AND candidate_manifest_digest=lower(candidate_manifest_digest)
  ),
  matched_count INTEGER NOT NULL CHECK(matched_count>=0),
  candidate_count INTEGER NOT NULL CHECK(candidate_count BETWEEN 1 AND 10000),
  screenshot_only_count INTEGER NOT NULL CHECK(screenshot_only_count>=0),
  duplicate_count INTEGER NOT NULL CHECK(duplicate_count>=0),
  attachment_active_count INTEGER NOT NULL CHECK(attachment_active_count>=0),
  not_ready_or_stale_count INTEGER NOT NULL CHECK(not_ready_or_stale_count>=0),
  processed_count INTEGER NOT NULL DEFAULT 0 CHECK(processed_count>=0 AND processed_count<=candidate_count),
  published_count INTEGER NOT NULL DEFAULT 0 CHECK(published_count>=0),
  skipped_duplicate_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_duplicate_count>=0),
  skipped_changed_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_changed_count>=0),
  skipped_not_ready_count INTEGER NOT NULL DEFAULT 0 CHECK(skipped_not_ready_count>=0),
  failed_count INTEGER NOT NULL DEFAULT 0 CHECK(failed_count>=0),
  cancelled_count INTEGER NOT NULL DEFAULT 0 CHECK(cancelled_count>=0),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  last_error_code TEXT,
  cancel_reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  started_at_ms INTEGER,
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  cancel_requested_at_ms INTEGER,
  CHECK((state IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED','FAILED'))=(completed_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(state NOT IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED') OR processed_count=candidate_count),
  CHECK(processed_count=published_count+skipped_duplicate_count+skipped_changed_count+
    skipped_not_ready_count+failed_count+cancelled_count)
);

CREATE TABLE review_bulk_approval_items (
  bulk_approval_id TEXT NOT NULL REFERENCES review_bulk_approvals(id),
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  ordinal INTEGER NOT NULL CHECK(ordinal>=0),
  expected_review_version INTEGER NOT NULL CHECK(expected_review_version>=1),
  expected_validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  expected_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  title_snapshot TEXT NOT NULL CHECK(length(title_snapshot) BETWEEN 1 AND 200),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  target_platform_name_snapshot TEXT NOT NULL CHECK(length(target_platform_name_snapshot) BETWEEN 1 AND 120),
  state TEXT NOT NULL CHECK(state IN (
    'PENDING','RUNNING','PUBLISHED','SKIPPED_DUPLICATE','SKIPPED_CHANGED','SKIPPED_NOT_READY',
    'FAILED_FINAL','CANCELLED'
  )),
  game_id TEXT REFERENCES games(id),
  review_event_id TEXT REFERENCES review_events(id),
  outcome_code TEXT,
  outcome_details_json TEXT CHECK(
    outcome_details_json IS NULL OR (json_valid(outcome_details_json) AND length(CAST(outcome_details_json AS BLOB))<=8192)
  ),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  started_at_ms INTEGER,
  completed_at_ms INTEGER,
  PRIMARY KEY(bulk_approval_id,import_item_id),
  UNIQUE(bulk_approval_id,ordinal),
  CHECK((state IN ('PENDING','RUNNING'))=(completed_at_ms IS NULL)),
  CHECK((state='PUBLISHED')=(game_id IS NOT NULL AND review_event_id IS NOT NULL))
);

CREATE TABLE review_draft_tags (
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  tag_id TEXT NOT NULL REFERENCES tags(id),
  assigned_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(review_draft_id,tag_id)
);

CREATE TABLE metadata_provider_cache (
  provider TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  current_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  expires_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  PRIMARY KEY(provider, request_digest)
);

CREATE TABLE metadata_provider_responses (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL CHECK(provider = 'HASHEOUS'),
  request_digest TEXT NOT NULL CHECK(length(request_digest) = 64),
  http_status INTEGER,
  outcome TEXT NOT NULL CHECK(outcome IN ('HIT','MISS','RATE_LIMITED','TIMEOUT','INVALID_RESPONSE','NETWORK_ERROR')),
  raw_response_blob_id TEXT REFERENCES blobs(id),
  fetched_at_ms INTEGER NOT NULL,
  expires_at_ms INTEGER NOT NULL
);

CREATE TABLE metadata_scrape_runs (
  id TEXT PRIMARY KEY,
  import_item_id TEXT REFERENCES import_items(id),
  game_id TEXT REFERENCES games(id),
  game_content_revision_id TEXT REFERENCES game_content_revisions(id),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  provider TEXT NOT NULL CHECK(provider IN ('HASHEOUS','NONE')),
  provider_config_version INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('RUNNING','COMPLETED','FAILED','CANCELLED')),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  completed_at_ms INTEGER,
  error_code TEXT,
  CHECK((import_item_id IS NOT NULL) != (game_id IS NOT NULL)),
  CHECK((game_id IS NULL) = (game_content_revision_id IS NULL)),
  CHECK((state = 'RUNNING') = (completed_at_ms IS NULL)),
  CHECK((state = 'FAILED') = (error_code IS NOT NULL))
);

CREATE TABLE metadata_scrape_query_attempts (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL REFERENCES metadata_scrape_runs(id),
  content_hash_evidence_id TEXT NOT NULL REFERENCES content_hash_evidence(id),
  provider_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  attempt_no INTEGER NOT NULL CHECK(attempt_no >= 1),
  source TEXT NOT NULL CHECK(source IN ('NETWORK','CACHE')),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(content_hash_evidence_id, attempt_no)
);

CREATE TABLE scrape_candidates (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL REFERENCES metadata_scrape_runs(id),
  primary_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  provider_game_id TEXT NOT NULL,
  normalized_metadata_json TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(scrape_run_id, provider_game_id)
);

CREATE TABLE scrape_candidate_hits (
  scrape_candidate_id TEXT NOT NULL REFERENCES scrape_candidates(id),
  query_attempt_id TEXT NOT NULL REFERENCES metadata_scrape_query_attempts(id),
  matched_hashes_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(scrape_candidate_id, query_attempt_id)
);

CREATE TABLE scrape_candidate_assets (
  id TEXT PRIMARY KEY,
  scrape_candidate_id TEXT NOT NULL REFERENCES scrape_candidates(id),
  provider_response_id TEXT NOT NULL REFERENCES metadata_provider_responses(id),
  provider_asset_id TEXT NOT NULL,
  kind_hint TEXT NOT NULL CHECK(kind_hint IN ('COVER','BACKGROUND','SCREENSHOT','UNKNOWN')),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  source_path TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('PENDING','FETCHING','READY','FAILED','CANCELLED')),
  blob_id TEXT REFERENCES blobs(id),
  width_px INTEGER,
  height_px INTEGER,
  media_type TEXT,
  error_code TEXT,
  fetched_at_ms INTEGER,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(scrape_candidate_id, provider_asset_id),
  CHECK((status = 'READY') = (blob_id IS NOT NULL AND width_px IS NOT NULL AND height_px IS NOT NULL AND media_type IS NOT NULL)),
  CHECK((status IN ('FAILED','CANCELLED')) = (error_code IS NOT NULL))
);

CREATE TABLE content_hash_evidence (
  id TEXT PRIMARY KEY,
  scrape_run_id TEXT NOT NULL REFERENCES metadata_scrape_runs(id),
  profile TEXT NOT NULL CHECK(profile IN ('RAW_FILE_V1','SINGLE_ARCHIVE_MEMBER_V1','ARCADE_DAT_ENTRIES_V1')),
  blob_id TEXT REFERENCES blobs(id),
  archive_blob_id TEXT,
  archive_entry_ordinal INTEGER,
  crc32 TEXT,
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  query_order INTEGER NOT NULL CHECK(query_order >= 0),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(scrape_run_id, profile, query_order),
  FOREIGN KEY(archive_blob_id, archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id, ordinal),
  CHECK((blob_id IS NOT NULL) != (archive_blob_id IS NOT NULL)),
  CHECK(crc32 IS NOT NULL OR md5 IS NOT NULL OR sha1 IS NOT NULL OR sha256 IS NOT NULL)
);

CREATE TABLE content_identity_claims (
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(platform_id, content_identity_digest)
);

CREATE TABLE dos_entries (
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  normalized_path TEXT NOT NULL,
  original_relative_path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('EXE','COM','BAT')),
  rank INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  direct_launch_safe INTEGER NOT NULL CHECK(direct_launch_safe IN (0,1)),
  PRIMARY KEY(game_content_revision_id, normalized_path)
);

