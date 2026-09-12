package recordstore

import (
	"context"
	"database/sql"
)

func CreateSaveStates(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateSaveStates)
}

func ValidateSaveStates(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, save_statesOwnership, keys)
}

const save_statesOwnership = `
SELECT CASE
WHEN ((
  EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=candidate.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND (
    candidate.disc_index IS NULL OR candidate.disc_index >= (
      SELECT count(*) FROM launch_external_files external
      WHERE external.launch_session_id=candidate.source_launch_session_id AND external.kind='DISC'
    )
  )
  OR NOT EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=candidate.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND candidate.disc_index IS NOT NULL
)) THEN 'save state disc index mismatch'
WHEN (NOT EXISTS(SELECT 1 FROM games WHERE id=candidate.game_id AND status='PUBLISHED')) THEN
'game payload owner is not published'
WHEN (NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  JOIN runtime_targets target ON target.provider_id=launch.provider_id AND
target.target_id=launch.target_id
  WHERE launch.id=candidate.source_launch_session_id
    AND launch.game_id=candidate.game_id AND launch.profile_id=candidate.profile_id
    AND launch.save_access='NORMAL'
    AND target.checkpoint_json IS NOT NULL
    AND EXISTS(
      SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
      WHERE readable.type='text' AND readable.value=candidate.checkpoint_format
    )
)) THEN 'invalid runtime checkpoint snapshot'
ELSE '' END
FROM save_states candidate
WHERE candidate.id=?`
