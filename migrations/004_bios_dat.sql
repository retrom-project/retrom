CREATE TABLE bios_requirements (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_artifact_id TEXT NOT NULL,
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
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(core_artifact_id, logical_name),
  FOREIGN KEY(core_artifact_id, core_id) REFERENCES core_artifacts(id, core_id),
  CHECK((source_kind = 'STATIC' AND dat_machine_name IS NULL) OR (source_kind = 'DAT_MACHINE' AND dat_machine_name IS NOT NULL))
);

CREATE TABLE bios_installations (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL REFERENCES bios_requirements(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
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
  updated_at_ms INTEGER NOT NULL,
  CHECK(NOT (status = 'INVALID' AND is_active = 1))
);
CREATE UNIQUE INDEX bios_installations_active ON bios_installations(requirement_id) WHERE is_active = 1;

CREATE TABLE dat_versions (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_artifact_id TEXT NOT NULL,
  source TEXT NOT NULL CHECK(source IN ('BUILTIN','USER')),
  builtin_relative_path TEXT,
  blob_id TEXT REFERENCES blobs(id),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  parser_version TEXT NOT NULL,
  compatibility_status TEXT NOT NULL CHECK(compatibility_status IN ('MATCHED','USER_CONFIRMED','UNKNOWN','INCOMPATIBLE')),
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
  UNIQUE(id, core_artifact_id),
  FOREIGN KEY(core_artifact_id, core_id) REFERENCES core_artifacts(id, core_id),
  CHECK((source = 'BUILTIN' AND builtin_relative_path IS NOT NULL AND blob_id IS NULL) OR (source = 'USER' AND builtin_relative_path IS NULL AND blob_id IS NOT NULL)),
  CHECK((parse_status = 'READY') = (parsed_at_ms IS NOT NULL)),
  CHECK(is_active = 0 OR (parse_status = 'READY' AND compatibility_status IN ('MATCHED','USER_CONFIRMED')))
);
CREATE UNIQUE INDEX dat_versions_active ON dat_versions(core_artifact_id) WHERE is_active = 1;
CREATE UNIQUE INDEX dat_versions_builtin_bytes ON dat_versions(core_artifact_id, sha256, parser_version) WHERE source = 'BUILTIN';

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

CREATE TABLE dat_bios_sets (
  dat_version_id TEXT NOT NULL,
  machine_name TEXT NOT NULL,
  bios_name TEXT NOT NULL,
  description TEXT NOT NULL,
  is_default INTEGER NOT NULL CHECK(is_default IN (0,1)),
  PRIMARY KEY(dat_version_id, machine_name, bios_name),
  FOREIGN KEY(dat_version_id, machine_name) REFERENCES dat_machines(dat_version_id, machine_name) ON DELETE CASCADE
);
CREATE UNIQUE INDEX dat_bios_sets_one_default ON dat_bios_sets(dat_version_id, machine_name) WHERE is_default = 1;

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
CREATE INDEX dat_rom_entries_crc32 ON dat_rom_entries(dat_version_id, crc32) WHERE crc32 IS NOT NULL;
CREATE INDEX dat_rom_entries_sha1 ON dat_rom_entries(dat_version_id, sha1) WHERE sha1 IS NOT NULL;

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

CREATE TABLE dat_import_jobs (
  job_id TEXT PRIMARY KEY REFERENCES jobs(id),
  dat_version_id TEXT NOT NULL UNIQUE REFERENCES dat_versions(id),
  base_dat_version_id TEXT REFERENCES dat_versions(id),
  diff_summary_json TEXT,
  diff_input_digest TEXT,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL
);
