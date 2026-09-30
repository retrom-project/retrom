CREATE TABLE runtime_preview_files (
  preview_session_id TEXT NOT NULL REFERENCES runtime_preview_sessions(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','EXTERNAL_FILE','DISC','PROJECT_FILE','RUNTIME_FILE')),
  logical_name TEXT NOT NULL CHECK(
    length(CAST(logical_name AS BLOB)) BETWEEN 1 AND 1024 AND
    logical_name NOT LIKE '%\%' AND logical_name NOT IN ('.','..') AND
    instr(logical_name,char(0))=0 AND
    (role IN ('PROJECT_FILE','RUNTIME_FILE') OR logical_name NOT LIKE '%/%')
  ),
  virtual_path TEXT,
  file_record TEXT NOT NULL,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(preview_session_id,role,logical_name),
  UNIQUE(preview_session_id,virtual_path),
  CHECK(
    (role IN ('PARENT','BIOS_BUNDLE') AND virtual_path IS NULL) OR
    (role IN ('PROJECT_FILE','RUNTIME_FILE') AND virtual_path IS NULL) OR
    (role IN ('EXTERNAL_FILE','DISC') AND virtual_path IS NOT NULL AND
      substr(virtual_path,1,1)='/' AND virtual_path NOT LIKE '%\%' AND
      virtual_path NOT LIKE '%?%' AND virtual_path NOT LIKE '%#%' AND
      instr(virtual_path,char(0))=0 AND virtual_path NOT LIKE '%//%' AND
      virtual_path NOT LIKE '%/./%' AND virtual_path NOT LIKE '%/../%' AND
      virtual_path NOT LIKE '%/.' AND virtual_path NOT LIKE '%/..')
  )
);


CREATE TABLE "runtime_preview_sessions" (
  id TEXT PRIMARY KEY,
  scope_id TEXT NOT NULL,
  content_revision TEXT NOT NULL,
  return_to TEXT NOT NULL,
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  actor_user_id TEXT NOT NULL REFERENCES users(id),
  idempotency_key TEXT NOT NULL,
  title TEXT NOT NULL CHECK(length(CAST(title AS BLOB)) BETWEEN 1 AND 800),
  content_kind TEXT NOT NULL REFERENCES content_kinds(id),
  content_file_record TEXT NOT NULL,
  content_logical_name TEXT NOT NULL CHECK(length(CAST(content_logical_name AS BLOB)) BETWEEN 1 AND 512),
  content_format TEXT NOT NULL CHECK(
    length(content_format) BETWEEN 2 AND 64 AND content_format=upper(content_format)
    AND content_format NOT GLOB '*[^A-Z0-9_]*'
  ),
  dependency_snapshot_json TEXT NOT NULL,
  default_dos_entry TEXT,
  checkpoint_payload_file_record TEXT,
  checkpoint_format TEXT CHECK(length(checkpoint_format) BETWEEN 1 AND 128),
  checkpoint_created_at_ms INTEGER CHECK(checkpoint_created_at_ms>=0),
  restore_from_preview_id TEXT,
  restore_payload_file_record TEXT,
  restore_checkpoint_format TEXT CHECK(length(restore_checkpoint_format) BETWEEN 1 AND 128),
  emulator_game_id INTEGER CHECK(emulator_game_id IS NULL OR emulator_game_id>0),
  credential_sha256 BLOB NOT NULL CHECK(length(credential_sha256)=32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','FINISHED','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms INTEGER NOT NULL CHECK(bootstrap_expires_at_ms>=0),
  hard_expires_at_ms INTEGER NOT NULL CHECK(hard_expires_at_ms>=bootstrap_expires_at_ms),
  activated_at_ms INTEGER,
  finished_at_ms INTEGER,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  UNIQUE(actor_user_id,idempotency_key),
  CHECK(state!='ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED'))=(finished_at_ms IS NOT NULL)),
  CHECK((checkpoint_payload_file_record IS NULL)=(checkpoint_format IS NULL)),
  CHECK((checkpoint_payload_file_record IS NULL)=(checkpoint_created_at_ms IS NULL)),
  CHECK((restore_payload_file_record IS NULL)=(restore_checkpoint_format IS NULL)),
  CHECK(restore_payload_file_record IS NULL OR restore_from_preview_id IS NOT NULL)
);

-- Pre-release bootstrap: create the current domain model directly.

CREATE TABLE "launch_content_files" (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 512),
  file_record TEXT NOT NULL,
  format_version TEXT NOT NULL CHECK(
    length(format_version) BETWEEN 2 AND 64 AND format_version=upper(format_version)
    AND format_version NOT GLOB '*[^A-Z0-9_]*'
  ),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(launch_session_id,logical_name)
);

CREATE TABLE isolated_runtime_bootstrap_tickets (
  ticket_sha256 BLOB PRIMARY KEY CHECK(length(ticket_sha256)=32),
  launch_id TEXT UNIQUE REFERENCES launch_sessions(id),
  preview_id TEXT UNIQUE REFERENCES runtime_preview_sessions(id) ON DELETE CASCADE,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=0),
  consumed_at_ms INTEGER CHECK(consumed_at_ms IS NULL OR consumed_at_ms BETWEEN 0 AND expires_at_ms),
  CHECK((launch_id IS NULL) <> (preview_id IS NULL))
);

CREATE TABLE isolated_runtime_capabilities (
  credential_sha256 BLOB PRIMARY KEY CHECK(length(credential_sha256)=32),
  launch_id TEXT UNIQUE REFERENCES launch_sessions(id),
  preview_id TEXT UNIQUE REFERENCES runtime_preview_sessions(id) ON DELETE CASCADE,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  issued_at_ms INTEGER NOT NULL CHECK(issued_at_ms>=0),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=issued_at_ms),
  revoked_at_ms INTEGER CHECK(revoked_at_ms IS NULL OR revoked_at_ms>=issued_at_ms),
  CHECK((launch_id IS NULL) <> (preview_id IS NULL))
);

CREATE TABLE "launch_sessions" (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  content_kind TEXT NOT NULL REFERENCES content_kinds(id),
  dependency_snapshot_json TEXT NOT NULL CHECK(json_valid(dependency_snapshot_json)),
  compatibility_code TEXT NOT NULL,
  save_state_id TEXT REFERENCES save_states(id),
  dos_entry_path TEXT,
  return_to TEXT NOT NULL,
  credential_sha256 BLOB NOT NULL CHECK(length(credential_sha256) = 32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','FINISHED','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms INTEGER NOT NULL,
  activated_at_ms INTEGER,
  finished_at_ms INTEGER,
  hard_expires_at_ms INTEGER NOT NULL,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  initial_disc_index INTEGER NOT NULL DEFAULT 0 CHECK(initial_disc_index BETWEEN 0 AND 7),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(hard_expires_at_ms >= bootstrap_expires_at_ms),
  CHECK(state != 'ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED')) = (finished_at_ms IS NOT NULL))
);

CREATE TABLE "launch_external_files" (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  virtual_path TEXT NOT NULL CHECK(length(virtual_path) BETWEEN 1 AND 512),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 255),
  file_record TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0), kind TEXT NOT NULL DEFAULT 'BIOS' CHECK(kind IN ('BIOS','BIOS_BUNDLE','PARENT','DISC')),
  PRIMARY KEY(launch_session_id, virtual_path),
  UNIQUE(launch_session_id, logical_name),
  CHECK(substr(virtual_path,1,1)='/' AND
        virtual_path NOT LIKE '%\%' AND
        virtual_path NOT LIKE '%?%' AND
        virtual_path NOT LIKE '%#%' AND
        instr(virtual_path,char(0))=0 AND
        virtual_path NOT LIKE '%//%' AND
        virtual_path NOT LIKE '%/./%' AND
        virtual_path NOT LIKE '%/../%' AND
        virtual_path NOT LIKE '%/.' AND
        virtual_path NOT LIKE '%/..'),
  CHECK(logical_name NOT LIKE '%/%' AND
        logical_name NOT LIKE '%\%' AND
        logical_name NOT IN ('','.','..') AND
        instr(logical_name,char(0))=0)
);

CREATE TABLE "save_states" (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  checkpoint_format TEXT NOT NULL CHECK(length(checkpoint_format) BETWEEN 1 AND 128),
  payload_file_record TEXT NOT NULL,
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256=lower(payload_sha256)),
  payload_size_bytes INTEGER NOT NULL CHECK(payload_size_bytes BETWEEN 1 AND 268435456),
  screenshot_file_record TEXT,
  name TEXT NOT NULL,
  active_duration_ms INTEGER NOT NULL CHECK(active_duration_ms >= 0),
  dos_entry_path TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  deleted_at_ms INTEGER,
  source_launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  disc_index INTEGER CHECK(disc_index BETWEEN 0 AND 7)
);

CREATE TABLE "play_sessions" (
  id TEXT PRIMARY KEY,
  launch_session_id TEXT NOT NULL UNIQUE REFERENCES launch_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  started_at_ms INTEGER NOT NULL,
  last_reported_at_ms INTEGER NOT NULL,
  ended_at_ms INTEGER,
  active_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(active_duration_ms >= 0),
  state TEXT NOT NULL CHECK(state IN ('ACTIVE','FINISHED','ABANDONED')),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  CHECK((state = 'ACTIVE') = (ended_at_ms IS NULL))
);
