DROP TRIGGER launch_content_files_immutable_update;

UPDATE launch_content_files AS locked
SET blob_id = (
      SELECT source.blob_id
      FROM launch_sessions AS launch
      JOIN variant_files AS source
        ON source.game_variant_revision_id = launch.game_variant_revision_id
       AND source.role = 'DOS_LAUNCH_BUNDLE'
       AND source.logical_name = 'game.zip'
      WHERE launch.id = locked.launch_session_id
    ),
    logical_name = 'game.zip',
    format_version = 'SOURCE_V1'
WHERE locked.format_version = 'RETROM_DOS_DIRECT_ZIP_V1'
  AND EXISTS (
    SELECT 1
    FROM launch_sessions AS launch
    JOIN variant_files AS source
      ON source.game_variant_revision_id = launch.game_variant_revision_id
     AND source.role = 'DOS_LAUNCH_BUNDLE'
     AND source.logical_name = 'game.zip'
    WHERE launch.id = locked.launch_session_id
  );

CREATE TRIGGER launch_content_files_immutable_update
BEFORE UPDATE ON launch_content_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
