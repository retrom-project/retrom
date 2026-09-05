-- Pre-release bootstrap: create the current domain model directly.

CREATE TABLE bios_installations (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL REFERENCES bios_requirements(id),
  blob_id TEXT REFERENCES blobs(id),
  original_filename TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
  md5 TEXT NOT NULL CHECK(length(md5) = 32),
  sha1 TEXT NOT NULL CHECK(length(sha1) = 40),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  validated_requirement_version INTEGER NOT NULL CHECK(validated_requirement_version >= 1),
  status TEXT NOT NULL CHECK(status IN ('MATCHED','HASH_WARNING','MISSING_ENTRY','INVALID')),
  validation_details_json TEXT NOT NULL,
  is_active INTEGER NOT NULL CHECK(is_active IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL, source_kind TEXT NOT NULL DEFAULT 'BROWSER_UPLOAD'
CHECK(source_kind IN ('BROWSER_UPLOAD','SERVER_DIRECTORY')), server_import_candidate_id TEXT REFERENCES server_bios_import_candidates(id),
  payload_released_at_ms INTEGER CHECK(payload_released_at_ms IS NULL OR payload_released_at_ms>=created_at_ms),
  CHECK(NOT (status = 'INVALID' AND is_active = 1)),
  CHECK(is_active=0 OR blob_id IS NOT NULL),
  CHECK((blob_id IS NULL)=(payload_released_at_ms IS NOT NULL))
);

CREATE TABLE dat_machines (
  dat_version_id TEXT NOT NULL REFERENCES dat_versions(id) ON DELETE CASCADE,
  machine_name TEXT NOT NULL,
  description TEXT NOT NULL,
  year TEXT NOT NULL,
  manufacturer TEXT NOT NULL,
  cloneof TEXT,
  romof TEXT,
  is_explicit_bios INTEGER NOT NULL CHECK(is_explicit_bios IN (0,1)),
  classification TEXT NOT NULL CHECK(classification IN ('NORMAL','EXPLICIT_BIOS','ROMOF_INFERENCE')),
  PRIMARY KEY(dat_version_id, machine_name)
);

CREATE TABLE dat_rom_entries (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
  name TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
  crc32 TEXT,
  sha1 TEXT,
  status TEXT CHECK(status IS NULL OR status IN ('GOOD','NODUMP','BADDUMP')),
  merge_name TEXT,
  bios_name TEXT,
  PRIMARY KEY(dat_version_id, machine_name, ordinal),
  FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE,
  FOREIGN KEY(dat_version_id, machine_name, bios_name) REFERENCES dat_bios_sets(dat_version_id, machine_name, bios_name),
  CHECK(status = 'NODUMP' OR crc32 IS NOT NULL OR sha1 IS NOT NULL)
);

CREATE TABLE dat_disk_entries (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
  name TEXT NOT NULL,
  sha1 TEXT,
  status TEXT CHECK(status IS NULL OR status IN ('GOOD','NODUMP','BADDUMP')),
  PRIMARY KEY(dat_version_id, machine_name, ordinal),
  FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE,
  CHECK(status = 'NODUMP' OR sha1 IS NOT NULL)
);

CREATE TABLE dat_bios_sets (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  bios_name TEXT NOT NULL,
  description TEXT NOT NULL,
  is_default INTEGER NOT NULL CHECK(is_default IN (0,1)),
  PRIMARY KEY(dat_version_id, machine_name, bios_name),
  FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE
);

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

CREATE TABLE runtime_asset_pack_definitions (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK(length(kind) BETWEEN 2 AND 64 AND kind NOT GLOB '*[^A-Za-z0-9_]*'),
  generation TEXT NOT NULL CHECK(
    length(generation) BETWEEN 2 AND 64 AND generation=upper(generation)
    AND generation NOT GLOB '*[^A-Z0-9_]*'
  ),
  declared_name TEXT NOT NULL CHECK(
    length(CAST(declared_name AS BLOB)) BETWEEN 1 AND 512 AND instr(declared_name,char(0))=0
  ),
  normalized_declared_name TEXT NOT NULL CHECK(
    length(CAST(normalized_declared_name AS BLOB)) BETWEEN 1 AND 512
    AND instr(normalized_declared_name,char(0))=0
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),
  required_layout_version TEXT NOT NULL CHECK(length(required_layout_version) BETWEEN 1 AND 160),
  origin TEXT NOT NULL CHECK(origin IN ('BUILTIN','CUSTOM')),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_by_user_id TEXT REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(generation,normalized_declared_name),
  UNIQUE(id,generation),
  CHECK((origin='BUILTIN' AND created_by_user_id IS NULL) OR (origin='CUSTOM' AND created_by_user_id IS NOT NULL))
);

CREATE TABLE runtime_asset_pack_installations (
  id TEXT PRIMARY KEY,
  definition_id TEXT NOT NULL REFERENCES runtime_asset_pack_definitions(id),
  files_digest TEXT NOT NULL CHECK(length(files_digest)=64 AND files_digest=lower(files_digest)),
  file_count INTEGER NOT NULL CHECK(file_count BETWEEN 1 AND 10000),
  total_bytes INTEGER NOT NULL CHECK(total_bytes BETWEEN 0 AND 536870912),
  bundle_blob_id TEXT REFERENCES blobs(id),
  bundle_sha256 TEXT CHECK(bundle_sha256 IS NULL OR length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  status TEXT NOT NULL CHECK(status IN ('VALIDATING','READY','FAILED','DELETE_PENDING','DELETED')),
  diagnostic_json TEXT NOT NULL CHECK(json_valid(diagnostic_json)),
  source_note TEXT CHECK(
    source_note IS NULL OR length(source_note)<=500 AND length(CAST(source_note AS BLOB))<=2000
    AND instr(source_note,char(0))=0
  ),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  validated_at_ms INTEGER CHECK(validated_at_ms IS NULL OR validated_at_ms>=created_at_ms),
  deleted_at_ms INTEGER CHECK(deleted_at_ms IS NULL OR deleted_at_ms>=created_at_ms),
  UNIQUE(id,definition_id),
  UNIQUE(definition_id,files_digest),
  CHECK((bundle_blob_id IS NULL)=(bundle_sha256 IS NULL)),
  CHECK(
    status='VALIDATING' AND validated_at_ms IS NULL AND deleted_at_ms IS NULL
    OR status='READY' AND validated_at_ms IS NOT NULL AND deleted_at_ms IS NULL AND bundle_blob_id IS NOT NULL
    OR status='FAILED' AND validated_at_ms IS NOT NULL AND deleted_at_ms IS NULL
    OR status='DELETE_PENDING' AND validated_at_ms IS NOT NULL AND deleted_at_ms IS NULL
    OR status='DELETED' AND validated_at_ms IS NOT NULL AND deleted_at_ms IS NOT NULL AND bundle_blob_id IS NULL
  )
);

CREATE TABLE runtime_asset_pack_files (
  installation_id TEXT NOT NULL REFERENCES runtime_asset_pack_installations(id),
  path TEXT NOT NULL CHECK(
    length(CAST(path AS BLOB)) BETWEEN 1 AND 4096 AND path NOT LIKE '/%' AND path NOT LIKE '%\%'
    AND instr(path,char(0))=0 AND path NOT LIKE '%//%' AND path NOT LIKE './%'
    AND path NOT LIKE '../%' AND path NOT LIKE '%/./%' AND path NOT LIKE '%/../%'
    AND path NOT LIKE '%/.' AND path NOT LIKE '%/..'
  ),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 9999),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 0 AND 536870912),
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256=lower(sha256)),
  PRIMARY KEY(installation_id,path),
  UNIQUE(installation_id,ordinal)
);

CREATE TABLE "bios_requirements" (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('STATIC','DAT_MACHINE')),
  dat_machine_name TEXT,
  logical_name TEXT NOT NULL,
  requirement_mode TEXT NOT NULL CHECK(requirement_mode IN ('REQUIRED','OPTIONAL','CONDITIONAL')),
  condition_code TEXT,
  activation_options_json TEXT,
  catalog_digest TEXT NOT NULL CHECK(length(catalog_digest) = 64),
  size_bytes INTEGER CHECK(size_bytes IS NULL OR size_bytes >= 0),
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  source_url TEXT NOT NULL,
  source_version TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL, delivery_kind TEXT NOT NULL DEFAULT 'BIOS_BUNDLE'
CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE')), emulator_path TEXT,
  UNIQUE(provider_id,target_id,logical_name),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK((source_kind = 'STATIC' AND dat_machine_name IS NULL) OR (source_kind = 'DAT_MACHINE' AND dat_machine_name IS NOT NULL))
);

CREATE TABLE "dat_versions" (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  builtin_relative_path TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  parser_version TEXT NOT NULL,
  parse_status TEXT NOT NULL CHECK(parse_status IN ('PENDING','PARSING','READY','FAILED','CANCELLED')),
  is_active INTEGER NOT NULL CHECK(is_active IN (0,1)),
  machine_count INTEGER,
  rom_entry_count INTEGER,
  disk_entry_count INTEGER,
  bios_set_count INTEGER,
  default_bios_set_count INTEGER,
  explicit_bios_machine_count INTEGER,
  base_dependency_target_count INTEGER,
  unresolved_relation_count INTEGER,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  parsed_at_ms INTEGER,
  activated_at_ms INTEGER,
  UNIQUE(id,provider_id,target_id),
  UNIQUE(provider_id,target_id,sha256,parser_version),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK((parse_status = 'READY') = (parsed_at_ms IS NOT NULL)),
  CHECK(is_active = 0 OR parse_status = 'READY')
);

CREATE TABLE "server_bios_import_items" (
  server_import_id TEXT NOT NULL REFERENCES server_imports(id),
  requirement_id TEXT NOT NULL REFERENCES bios_requirements(id),
  requirement_version INTEGER NOT NULL CHECK(requirement_version>=1),
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_name_snapshot TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
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
  CHECK((state IN ('PENDING','EVALUATING'))=(completed_at_ms IS NULL)),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id)
);

CREATE TABLE "game_variant_runtime_packs" (
  game_variant_id TEXT NOT NULL REFERENCES game_variants(id),
  slot INTEGER NOT NULL CHECK(slot BETWEEN 0 AND 3),
  declared_name TEXT NOT NULL CHECK(length(CAST(declared_name AS BLOB)) BETWEEN 1 AND 512),
  normalized_declared_name TEXT NOT NULL CHECK(length(CAST(normalized_declared_name AS BLOB)) BETWEEN 1 AND 512),
  definition_id TEXT NOT NULL REFERENCES runtime_asset_pack_definitions(id),
  installation_id TEXT NOT NULL,
  PRIMARY KEY(game_variant_id,slot),
  FOREIGN KEY(installation_id,definition_id)
    REFERENCES runtime_asset_pack_installations(id,definition_id)
);
