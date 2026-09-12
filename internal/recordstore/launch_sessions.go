package recordstore

import (
	"context"
	"database/sql"
)

func CreateLaunchSessions(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateLaunchSessions)
}

func ValidateLaunchSessions(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, launch_sessionsOwnership, keys)
}

const launch_sessionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
    AND provider.bundle_sha256=candidate.bundle_sha256
)
OR NOT EXISTS(
  SELECT 1 FROM games game
  JOIN game_variants variant ON variant.game_id=game.id
  WHERE game.id=candidate.game_id AND game.status='PUBLISHED'
    AND variant.core_id=candidate.core_id AND variant.provider_id=candidate.provider_id
    AND variant.target_id=candidate.target_id AND variant.status='READY'
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM launch_sessions candidate
WHERE candidate.id=?`
