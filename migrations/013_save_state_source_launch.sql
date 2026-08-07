ALTER TABLE save_states
ADD COLUMN source_launch_session_id TEXT REFERENCES launch_sessions(id);

CREATE INDEX save_states_source_launch
ON save_states(source_launch_session_id, created_at_ms DESC, id DESC)
WHERE source_launch_session_id IS NOT NULL AND deleted_at_ms IS NULL;

CREATE TRIGGER save_states_source_launch_insert
BEFORE INSERT ON save_states
WHEN NEW.source_launch_session_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1
    FROM launch_sessions launch
    JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
    WHERE launch.id=NEW.source_launch_session_id
    AND launch.profile_id=NEW.profile_id
    AND launch.game_id=NEW.game_id
    AND launch.game_variant_revision_id=NEW.game_variant_revision_id
    AND launch.core_artifact_id=NEW.core_artifact_id
    AND launch.dos_entry_path IS NEW.dos_entry_path
    AND revision.dat_version_id IS NEW.dat_version_id
  ) THEN RAISE(ABORT, 'save state source launch mismatch') END;
END;

CREATE TRIGGER save_states_source_launch_update
BEFORE UPDATE OF source_launch_session_id,profile_id,game_id,game_variant_revision_id,core_artifact_id,dat_version_id,dos_entry_path
ON save_states
WHEN NEW.source_launch_session_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1
    FROM launch_sessions launch
    JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
    WHERE launch.id=NEW.source_launch_session_id
    AND launch.profile_id=NEW.profile_id
    AND launch.game_id=NEW.game_id
    AND launch.game_variant_revision_id=NEW.game_variant_revision_id
    AND launch.core_artifact_id=NEW.core_artifact_id
    AND launch.dos_entry_path IS NEW.dos_entry_path
    AND revision.dat_version_id IS NEW.dat_version_id
  ) THEN RAISE(ABORT, 'save state source launch mismatch') END;
END;

CREATE TRIGGER save_states_source_launch_immutable
BEFORE UPDATE OF source_launch_session_id ON save_states
WHEN OLD.source_launch_session_id IS NOT NEW.source_launch_session_id
BEGIN SELECT RAISE(ABORT, 'save state source launch is immutable'); END;
