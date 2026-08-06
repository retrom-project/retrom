CREATE TABLE launch_sessions (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  save_state_id TEXT REFERENCES save_states(id),
  dos_entry_path TEXT,
  persistent_save_base_revision_id TEXT REFERENCES persistent_save_revisions(id),
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
  CHECK(hard_expires_at_ms >= bootstrap_expires_at_ms),
  CHECK(state != 'ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED')) = (finished_at_ms IS NOT NULL))
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

CREATE TABLE save_states (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  dat_version_id TEXT REFERENCES dat_versions(id),
  dos_entry_path TEXT,
  state_blob_id TEXT NOT NULL REFERENCES blobs(id),
  screenshot_blob_id TEXT NOT NULL REFERENCES blobs(id),
  name TEXT NOT NULL,
  active_duration_ms INTEGER NOT NULL CHECK(active_duration_ms >= 0),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  deleted_at_ms INTEGER
);
CREATE INDEX save_states_library ON save_states(profile_id, game_id, created_at_ms DESC, id DESC);

CREATE TABLE persistent_saves (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  kind TEXT NOT NULL CHECK(kind IN ('CORE_SAVE','DOS_OVERLAY')),
  current_revision_id TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(profile_id, game_variant_revision_id, kind),
  FOREIGN KEY(current_revision_id) REFERENCES persistent_save_revisions(id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE persistent_save_revisions (
  id TEXT PRIMARY KEY,
  persistent_save_id TEXT NOT NULL REFERENCES persistent_saves(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  client_sequence INTEGER NOT NULL CHECK(client_sequence >= 1),
  source_event TEXT NOT NULL CHECK(source_event IN ('AUTO_INTERVAL','MANUAL_EXPORT','EXIT')),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(source_launch_session_id, client_sequence),
  UNIQUE(id, persistent_save_id)
);

CREATE TRIGGER play_session_events_immutable_update BEFORE UPDATE ON play_session_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER play_session_events_immutable_delete BEFORE DELETE ON play_session_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER persistent_save_revisions_immutable_update BEFORE UPDATE ON persistent_save_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER persistent_save_revisions_immutable_delete BEFORE DELETE ON persistent_save_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
