-- Fresh native-save metadata; application writes own initialization and binding.
CREATE TABLE game_save_versions (
  save_state_id TEXT PRIMARY KEY REFERENCES save_states(id) ON DELETE CASCADE,
  data_version INTEGER NOT NULL DEFAULT 1 CHECK(data_version>=1),
  last_synced_at_ms INTEGER CHECK(last_synced_at_ms>=0),
  last_writer_launch_session_id TEXT REFERENCES launch_sessions(id) ON DELETE SET NULL
);

CREATE TABLE launch_game_save_bindings (
  launch_session_id TEXT PRIMARY KEY REFERENCES launch_sessions(id) ON DELETE CASCADE,
  save_state_id TEXT REFERENCES save_states(id) ON DELETE SET NULL,
  initial_active_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(initial_active_duration_ms>=0),
  expected_data_version INTEGER NOT NULL DEFAULT 0 CHECK(expected_data_version>=0),
  restore_payload_blob_id TEXT REFERENCES blobs(id),
  restore_checkpoint_format TEXT,
  CHECK((restore_payload_blob_id IS NULL)=(restore_checkpoint_format IS NULL))
);
CREATE INDEX launch_game_save_target ON launch_game_save_bindings(save_state_id);
