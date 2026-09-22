-- Pre-release bootstrap: create the current domain model directly.

CREATE TABLE "launch_content_files" (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 512),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
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
  preview_id TEXT UNIQUE REFERENCES review_preview_sessions(id) ON DELETE CASCADE,
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
  preview_id TEXT UNIQUE REFERENCES review_preview_sessions(id) ON DELETE CASCADE,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  issued_at_ms INTEGER NOT NULL CHECK(issued_at_ms>=0),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=issued_at_ms),
  revoked_at_ms INTEGER CHECK(revoked_at_ms IS NULL OR revoked_at_ms>=issued_at_ms),
  CHECK((launch_id IS NULL) <> (preview_id IS NULL))
);

CREATE TABLE play_session_events (
  play_session_id TEXT NOT NULL REFERENCES play_sessions(id),
  client_sequence INTEGER NOT NULL CHECK(client_sequence >= 0),
  event_kind TEXT NOT NULL CHECK(event_kind IN ('START','HEARTBEAT','FINISH')),
  client_observed_at_ms INTEGER NOT NULL,
  server_received_at_ms INTEGER NOT NULL,
  running INTEGER NOT NULL CHECK(running IN (0,1)),
  visible INTEGER NOT NULL CHECK(visible IN (0,1)),
  paused INTEGER NOT NULL CHECK(paused IN (0,1)),
  accepted_duration_ms INTEGER NOT NULL CHECK(accepted_duration_ms BETWEEN 0 AND 45000),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(play_session_id, client_sequence),
  CHECK((event_kind = 'START') = (client_sequence = 0)),
  CHECK(event_kind != 'START' OR accepted_duration_ms = 0)
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
  idle_expires_at_ms INTEGER,
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
  blob_id TEXT NOT NULL REFERENCES blobs(id),
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
  payload_blob_id TEXT NOT NULL REFERENCES blobs(id),
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256=lower(payload_sha256)),
  payload_size_bytes INTEGER NOT NULL CHECK(payload_size_bytes BETWEEN 1 AND 268435456),
  screenshot_blob_id TEXT REFERENCES blobs(id),
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
  last_heartbeat_at_ms INTEGER NOT NULL,
  ended_at_ms INTEGER,
  active_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(active_duration_ms >= 0),
  last_client_sequence INTEGER NOT NULL DEFAULT 0 CHECK(last_client_sequence >= 0),
  state TEXT NOT NULL CHECK(state IN ('ACTIVE','FINISHED','ABANDONED')),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  CHECK((state = 'ACTIVE') = (ended_at_ms IS NULL))
);
