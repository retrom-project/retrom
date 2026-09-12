package recordstore

import (
	"context"
	"database/sql"
)

func CreateLaunchExternalFiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "launch_session_id,virtual_path", ValidateLaunchExternalFiles)
}

func ValidateLaunchExternalFiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, launch_external_filesOwnership, keys)
}

const launch_external_filesOwnership = `
SELECT CASE
WHEN (candidate.kind='DISC' AND NOT EXISTS(
  SELECT 1 FROM launch_content_files content
  WHERE content.launch_session_id=candidate.launch_session_id
  AND content.format_version='RETROM_MULTIDISC_M3U_V1'
)) THEN 'disc external file requires multi-disc launch content'
WHEN (NOT EXISTS(
  SELECT 1 FROM launch_sessions launch LEFT JOIN games game ON game.id=launch.game_id
  WHERE launch.id=candidate.launch_session_id AND game.status='PUBLISHED'
)) THEN 'launch external owner is invalid'
ELSE '' END
FROM launch_external_files candidate
WHERE candidate.launch_session_id=? AND candidate.virtual_path=?`
