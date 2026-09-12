package recordstore

import (
	"context"
	"database/sql"
)

func CreateLaunchContentFiles(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "launch_session_id,logical_name", ValidateLaunchContentFiles)
}

func ValidateLaunchContentFiles(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, launch_content_filesOwnership, keys)
}

const launch_content_filesOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM launch_sessions launch LEFT JOIN games game ON game.id=launch.game_id
  WHERE launch.id=candidate.launch_session_id AND game.status='PUBLISHED'
)) THEN 'launch content owner is invalid'
ELSE '' END
FROM launch_content_files candidate
WHERE candidate.launch_session_id=? AND candidate.logical_name=?`
