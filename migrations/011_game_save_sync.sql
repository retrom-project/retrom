-- Native game data is a mutable slot, with one optimistic writer binding per launch.
ALTER TABLE save_states ADD COLUMN data_version INTEGER NOT NULL DEFAULT 1 CHECK(data_version>=1);

ALTER TABLE save_states ADD COLUMN last_synced_at_ms INTEGER CHECK(last_synced_at_ms>=created_at_ms);

ALTER TABLE save_states ADD COLUMN last_writer_launch_session_id TEXT REFERENCES launch_sessions(id) ON DELETE SET NULL;

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

-- This freezes the input before any other launch can update the selected slot.
CREATE TRIGGER launch_game_save_bind AFTER INSERT ON launch_sessions
WHEN NEW.game_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM runtime_targets target WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
  AND json_extract(target.checkpoint_json,'$.semantics')='GAME_SAVE'
)
BEGIN
  INSERT INTO launch_game_save_bindings(
    launch_session_id,save_state_id,expected_data_version,restore_payload_blob_id,restore_checkpoint_format,initial_active_duration_ms)
  SELECT NEW.id,save.id,COALESCE(save.data_version,0),save.payload_blob_id,save.checkpoint_format,COALESCE(save.active_duration_ms,0)
  FROM (SELECT 1) LEFT JOIN save_states save ON save.id=NEW.save_state_id
    AND save.profile_id=NEW.profile_id AND save.game_id=NEW.game_id AND save.deleted_at_ms IS NULL;
END;

-- Preserve existing saves; running native launches adopt the same rule on upgrade.
INSERT INTO launch_game_save_bindings(
  launch_session_id,save_state_id,expected_data_version,restore_payload_blob_id,restore_checkpoint_format,initial_active_duration_ms)
SELECT launch.id,save.id,COALESCE(save.data_version,0),save.payload_blob_id,save.checkpoint_format,COALESCE(save.active_duration_ms,0)
FROM launch_sessions launch
JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
LEFT JOIN save_states save ON save.id=launch.save_state_id
 AND save.profile_id=launch.profile_id AND save.game_id=launch.game_id AND save.deleted_at_ms IS NULL
WHERE launch.game_id IS NOT NULL AND launch.state IN ('CREATED','ACTIVE')
 AND json_extract(target.checkpoint_json,'$.semantics')='GAME_SAVE';

CREATE TRIGGER launch_game_save_release_input AFTER UPDATE OF state ON launch_sessions
WHEN NEW.state NOT IN ('CREATED','ACTIVE')
BEGIN
  UPDATE launch_game_save_bindings SET restore_payload_blob_id=NULL,restore_checkpoint_format=NULL
  WHERE launch_session_id=NEW.id;
END;
