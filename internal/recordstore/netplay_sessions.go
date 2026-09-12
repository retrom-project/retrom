package recordstore

import (
	"context"
	"database/sql"
)

func CreateNetplaySessions(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateNetplaySessions)
}

func ValidateNetplaySessions(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, netplay_sessionsOwnership, keys)
}

const netplay_sessionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
    AND provider.bundle_sha256=candidate.bundle_sha256
    AND json_extract(target.capabilities_json,'$.netplayPort')=1
)) THEN 'invalid runtime netplay snapshot'
WHEN (NOT EXISTS(
  SELECT 1 FROM netplay_rooms room
  JOIN game_variants variant ON variant.id=candidate.game_variant_id
  JOIN games game ON game.id=variant.game_id
  WHERE room.id=candidate.room_id AND room.selected_game_id=candidate.game_id
    AND room.selected_game_variant_id=candidate.game_variant_id
    AND room.netplay_profile_id=candidate.netplay_profile_id AND
room.profile_digest=candidate.profile_digest
    AND variant.game_id=candidate.game_id AND variant.status='READY' AND game.status='PUBLISHED'
    AND variant.provider_id=candidate.provider_id AND variant.target_id=candidate.target_id
)) THEN 'invalid netplay session snapshot'
ELSE '' END
FROM netplay_sessions candidate
WHERE candidate.id=?`
