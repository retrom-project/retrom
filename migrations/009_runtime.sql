-- Clean pre-release baseline: runtime.

CREATE TABLE launch_sessions (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  purpose TEXT NOT NULL DEFAULT 'PRODUCT' CHECK(purpose IN ('PRODUCT','RPG_RUNTIME_VALIDATION')),
  game_id TEXT REFERENCES games(id),
  game_content_revision_id TEXT REFERENCES game_content_revisions(id),
  game_variant_revision_id TEXT REFERENCES game_variant_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  route_key TEXT NOT NULL CHECK(length(route_key) BETWEEN 1 AND 160),
  effective_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  rpgmaker_runtime_validation_id TEXT REFERENCES rpgmaker_runtime_validations(id),
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
  version INTEGER NOT NULL DEFAULT 1, initial_disc_index INTEGER NOT NULL DEFAULT 0 CHECK(initial_disc_index BETWEEN 0 AND 7), netplay_session_id TEXT REFERENCES netplay_sessions(id), netplay_player_no INTEGER CHECK(netplay_player_no IS NULL OR netplay_player_no BETWEEN 1 AND 4), save_access TEXT NOT NULL DEFAULT 'NORMAL'
  CHECK(save_access IN ('NORMAL','NETPLAY_DISABLED')),
  CHECK(hard_expires_at_ms >= bootstrap_expires_at_ms),
  CHECK(state != 'ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED')) = (finished_at_ms IS NOT NULL)),
  CHECK(
    purpose='PRODUCT' AND game_id IS NOT NULL AND game_content_revision_id IS NOT NULL
      AND game_variant_revision_id IS NOT NULL AND effective_source_snapshot_id IS NULL
      AND rpgmaker_runtime_validation_id IS NULL
    OR purpose='RPG_RUNTIME_VALIDATION' AND game_id IS NULL AND game_content_revision_id IS NULL
      AND game_variant_revision_id IS NULL AND effective_source_snapshot_id IS NOT NULL
      AND rpgmaker_runtime_validation_id IS NOT NULL AND save_state_id IS NULL
      AND dos_entry_path IS NULL AND netplay_session_id IS NULL AND netplay_player_no IS NULL
      AND save_access='NORMAL' AND initial_disc_index=0
  )
);

CREATE TABLE "launch_content_files" (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 512),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  format_version TEXT NOT NULL CHECK(format_version IN (
    'SOURCE_V1','RETROM_DOS_DIRECT_ZIP_V1','RETROM_MULTIDISC_M3U_V1',
    'RPG_MAKER_PROJECT_V1'
  )),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(launch_session_id,logical_name)
);

CREATE TABLE launch_external_files (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  virtual_path TEXT NOT NULL CHECK(length(virtual_path) BETWEEN 1 AND 512),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 255),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0), kind TEXT NOT NULL DEFAULT 'BIOS' CHECK(kind IN ('BIOS','DISC')),
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

CREATE TABLE save_states (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  adapter_abi TEXT NOT NULL,
  dependency_snapshot_sha256 TEXT NOT NULL CHECK(
    length(dependency_snapshot_sha256)=64 AND dependency_snapshot_sha256=lower(dependency_snapshot_sha256)
  ),
  dat_version_id TEXT REFERENCES dat_versions(id),
  dos_entry_path TEXT,
  payload_blob_id TEXT NOT NULL REFERENCES blobs(id),
  payload_kind TEXT NOT NULL CHECK(payload_kind IN ('RUNTIME_STATE','NATIVE_SAVE_BUNDLE_V1')),
  native_profile TEXT CHECK(native_profile IS NULL OR native_profile IN ('EASYRPG_V1','RPGMV_V1','RPGMZ_V1')),
  resume_slot INTEGER CHECK(resume_slot IS NULL OR resume_slot BETWEEN 1 AND 2147483647),
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256=lower(payload_sha256)),
  payload_size_bytes INTEGER NOT NULL CHECK(payload_size_bytes BETWEEN 1 AND 268435456),
  screenshot_blob_id TEXT REFERENCES blobs(id),
  name TEXT NOT NULL,
  active_duration_ms INTEGER NOT NULL CHECK(active_duration_ms >= 0),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  deleted_at_ms INTEGER,
  source_launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  disc_index INTEGER CHECK(disc_index BETWEEN 0 AND 7),
  CHECK(
    payload_kind='RUNTIME_STATE' AND native_profile IS NULL AND resume_slot IS NULL
    OR payload_kind='NATIVE_SAVE_BUNDLE_V1' AND native_profile IS NOT NULL AND resume_slot IS NOT NULL
  )
);

CREATE TABLE rpgmaker_runtime_validation_checkpoints (
  validation_id TEXT PRIMARY KEY REFERENCES rpgmaker_runtime_validations(id),
  payload_blob_id TEXT NOT NULL REFERENCES blobs(id),
  payload_kind TEXT NOT NULL CHECK(payload_kind IN ('RUNTIME_STATE','NATIVE_SAVE_BUNDLE_V1')),
  native_profile TEXT CHECK(native_profile IS NULL OR native_profile IN ('EASYRPG_V1','RPGMV_V1','RPGMZ_V1')),
  resume_slot INTEGER CHECK(resume_slot IS NULL OR resume_slot BETWEEN 1 AND 2147483647),
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256=lower(payload_sha256)),
  size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 268435456),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  CHECK(
    payload_kind='RUNTIME_STATE' AND native_profile IS NULL AND resume_slot IS NULL
    OR payload_kind='NATIVE_SAVE_BUNDLE_V1' AND native_profile IS NOT NULL AND resume_slot IS NOT NULL
  )
);

CREATE TABLE isolated_runtime_bootstrap_tickets (
  ticket_sha256 BLOB PRIMARY KEY CHECK(length(ticket_sha256)=32),
  launch_id TEXT NOT NULL UNIQUE REFERENCES launch_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=0),
  consumed_at_ms INTEGER CHECK(consumed_at_ms IS NULL OR consumed_at_ms BETWEEN 0 AND expires_at_ms)
);

CREATE TABLE isolated_runtime_capabilities (
  credential_sha256 BLOB PRIMARY KEY CHECK(length(credential_sha256)=32),
  launch_id TEXT NOT NULL UNIQUE REFERENCES launch_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  expected_origin TEXT NOT NULL CHECK(
    expected_origin LIKE 'https://%' OR expected_origin LIKE 'http://%localhost:%'
  ),
  issued_at_ms INTEGER NOT NULL CHECK(issued_at_ms>=0),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=issued_at_ms),
  revoked_at_ms INTEGER CHECK(revoked_at_ms IS NULL OR revoked_at_ms>=issued_at_ms)
);

CREATE TABLE play_sessions (
  id TEXT PRIMARY KEY,
  launch_session_id TEXT NOT NULL UNIQUE REFERENCES launch_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
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

CREATE TABLE netplay_rooms (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  host_profile_id TEXT NOT NULL REFERENCES profiles(id),
  state TEXT NOT NULL CHECK(state IN ('DRAFT','WAITING','STARTING','RUNNING','ENDED','EXPIRED')),
  selected_game_id TEXT REFERENCES games(id),
  selected_game_variant_revision_id TEXT REFERENCES game_variant_revisions(id),
  netplay_profile_id TEXT,
  profile_digest TEXT CHECK(profile_digest IS NULL OR profile_digest GLOB '[0-9a-f]*' AND length(profile_digest)=64),
  max_players INTEGER CHECK(max_players IS NULL OR max_players BETWEEN 2 AND 4),
  current_session_id TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  ended_at_ms INTEGER,
  end_reason TEXT CHECK(end_reason IS NULL OR end_reason IN (
    'NORMAL','USER_EXIT','HOST_CLOSED','HOST_LOST','PEER_TIMEOUT','AUTH_REVOKED','START_TIMEOUT',
    'PREPARE_FAILED','PROFILE_REVOKED','SERVER_RESTARTED','RESTORE','HARD_EXPIRED',
    'ROLLBACK_WINDOW_EXCEEDED','STATE_RING_CAPACITY_EXCEEDED','STATE_TRANSFER_TIMEOUT',
    'STATE_INVALID','NETPLAY_UNSTABLE','PEER_TOO_SLOW','PROTOCOL_VIOLATION','INTERNAL_ERROR','GAME_DELETED',
    'GAME_CONTENT_REPLACED','BIOS_REPLACED'
  )),
  CHECK(
    selected_game_id IS NULL AND selected_game_variant_revision_id IS NULL AND netplay_profile_id IS NULL
      AND profile_digest IS NULL AND max_players IS NULL OR
    selected_game_id IS NOT NULL AND selected_game_variant_revision_id IS NOT NULL AND netplay_profile_id IS NOT NULL
      AND profile_digest IS NOT NULL AND max_players IS NOT NULL
  ),
  CHECK(state!='DRAFT' OR selected_game_id IS NULL),
  CHECK(state NOT IN ('WAITING','STARTING','RUNNING') OR selected_game_id IS NOT NULL),
  CHECK((state IN ('STARTING','RUNNING'))=(current_session_id IS NOT NULL)),
  CHECK((state IN ('ENDED','EXPIRED'))=(ended_at_ms IS NOT NULL)),
  CHECK((ended_at_ms IS NULL)=(end_reason IS NULL)),
  CHECK(ended_at_ms IS NULL OR ended_at_ms>=created_at_ms)
);

CREATE TABLE netplay_room_members (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  role TEXT NOT NULL CHECK(role IN ('HOST','GUEST')),
  player_no INTEGER NOT NULL CHECK(player_no BETWEEN 1 AND 4),
  ready INTEGER NOT NULL DEFAULT 0 CHECK(ready IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  joined_at_ms INTEGER NOT NULL CHECK(joined_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=joined_at_ms),
  left_at_ms INTEGER,
  leave_reason TEXT CHECK(leave_reason IS NULL OR leave_reason IN ('USER_LEFT','HOST_KICKED','SESSION_ENDED','ROOM_ENDED','AUTH_REVOKED')),
  UNIQUE(room_id,profile_id),
  CHECK((role='HOST')=(player_no=1)),
  CHECK((left_at_ms IS NULL)=(leave_reason IS NULL)),
  CHECK(left_at_ms IS NULL OR left_at_ms>=joined_at_ms),
  CHECK(left_at_ms IS NULL OR ready=0)
);

CREATE TABLE netplay_sessions (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  session_no INTEGER NOT NULL CHECK(session_no>=1),
  state TEXT NOT NULL CHECK(state IN (
    'PREPARING','LOADING','SYNCHRONIZING','RUNNING','PAUSED_RECONNECT','RESYNCHRONIZING','FINISHED','FAILED'
  )),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  netplay_profile_id TEXT NOT NULL,
  profile_json TEXT NOT NULL CHECK(json_valid(profile_json) AND json_type(profile_json)='object'),
  profile_digest TEXT NOT NULL CHECK(profile_digest GLOB '[0-9a-f]*' AND length(profile_digest)=64),
  player_count INTEGER NOT NULL CHECK(player_count BETWEEN 2 AND 4),
  occupied_seat_mask INTEGER NOT NULL CHECK(occupied_seat_mask BETWEEN 3 AND 15 AND (occupied_seat_mask & 1)=1),
  authority_player_no INTEGER NOT NULL DEFAULT 1 CHECK(authority_player_no=1),
  resync_count INTEGER NOT NULL DEFAULT 0 CHECK(resync_count>=0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  started_at_ms INTEGER,
  finished_at_ms INTEGER,
  end_reason TEXT CHECK(end_reason IS NULL OR end_reason IN (
    'NORMAL','USER_EXIT','HOST_CLOSED','HOST_LOST','PEER_TIMEOUT','AUTH_REVOKED','START_TIMEOUT',
    'PREPARE_FAILED','PROFILE_REVOKED','SERVER_RESTARTED','RESTORE','HARD_EXPIRED',
    'ROLLBACK_WINDOW_EXCEEDED','STATE_RING_CAPACITY_EXCEEDED','STATE_TRANSFER_TIMEOUT',
    'STATE_INVALID','NETPLAY_UNSTABLE','PEER_TOO_SLOW','PROTOCOL_VIOLATION','INTERNAL_ERROR','GAME_DELETED',
    'GAME_CONTENT_REPLACED','BIOS_REPLACED'
  )),
  UNIQUE(room_id,session_no),
  CHECK((state IN ('FINISHED','FAILED'))=(finished_at_ms IS NOT NULL)),
  CHECK((finished_at_ms IS NULL)=(end_reason IS NULL)),
  CHECK(started_at_ms IS NULL OR started_at_ms>=created_at_ms),
  CHECK(finished_at_ms IS NULL OR finished_at_ms>=created_at_ms)
);

CREATE TABLE netplay_session_participants (
  netplay_session_id TEXT NOT NULL REFERENCES netplay_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  room_member_id TEXT NOT NULL REFERENCES netplay_room_members(id),
  player_no INTEGER NOT NULL CHECK(player_no BETWEEN 1 AND 4),
  launch_session_id TEXT UNIQUE REFERENCES launch_sessions(id),
  credential_sha256 BLOB CHECK(credential_sha256 IS NULL OR length(credential_sha256)=32),
  state TEXT NOT NULL CHECK(state IN (
    'LOCKED','LAUNCH_READY','RUNTIME_READY','SYNCHRONIZED','CONNECTED','DISCONNECTED','LEFT'
  )),
  credential_generation INTEGER NOT NULL DEFAULT 0 CHECK(credential_generation>=0),
  disconnected_at_ms INTEGER,
  lease_expires_at_ms INTEGER,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  PRIMARY KEY(netplay_session_id,profile_id),
  UNIQUE(netplay_session_id,player_no),
  UNIQUE(netplay_session_id,room_member_id),
  CHECK(
    state='LOCKED' AND launch_session_id IS NULL AND credential_sha256 IS NULL AND credential_generation=0 OR
    state='LEFT' AND (
      launch_session_id IS NULL AND credential_sha256 IS NULL AND credential_generation=0 OR
      launch_session_id IS NOT NULL AND credential_sha256 IS NOT NULL AND credential_generation>=1
    ) OR
    state NOT IN ('LOCKED','LEFT') AND launch_session_id IS NOT NULL AND credential_sha256 IS NOT NULL AND credential_generation>=1
  ),
  CHECK(
    state='DISCONNECTED' AND disconnected_at_ms IS NOT NULL AND lease_expires_at_ms IS NOT NULL
      AND lease_expires_at_ms>=disconnected_at_ms OR
    state!='DISCONNECTED' AND disconnected_at_ms IS NULL AND lease_expires_at_ms IS NULL
  )
);

CREATE TABLE netplay_events (
  id INTEGER PRIMARY KEY,
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  netplay_session_id TEXT REFERENCES netplay_sessions(id),
  profile_id TEXT REFERENCES profiles(id),
  player_no INTEGER CHECK(player_no IS NULL OR player_no BETWEEN 1 AND 4),
  event_type TEXT NOT NULL CHECK(event_type IN (
    'ROOM_CREATED','GAME_SELECTED','GAME_CLEARED','MEMBER_JOINED','SEAT_CHANGED','READY_CHANGED',
    'MEMBER_LEFT','MEMBER_KICKED','SESSION_CREATED','SESSION_STATE_CHANGED',
    'PARTICIPANT_STATE_CHANGED','PAUSED','RESUMED','RESYNCED','ROOM_ENDED','ROOM_EXPIRED'
  )),
  result_code TEXT CHECK(result_code IS NULL OR length(CAST(result_code AS BLOB)) BETWEEN 1 AND 64),
  data_json TEXT NOT NULL CHECK(json_valid(data_json) AND json_type(data_json)='object'),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0)
);
