package recordstore

import (
	"context"
	"database/sql"
)

func CreateNetplayRooms(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateNetplayRooms)
}

func ValidateNetplayRooms(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, netplay_roomsOwnership, keys)
}

const netplay_roomsOwnership = `
SELECT CASE
WHEN (candidate.current_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_sessions session WHERE session.id=candidate.current_session_id AND
session.room_id=candidate.id
)) THEN 'invalid netplay current session'
WHEN (candidate.selected_game_variant_id IS NOT NULL AND NOT EXISTS(
  SELECT 1
  FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=candidate.selected_game_variant_id AND variant.game_id=candidate.selected_game_id
    AND variant.status='READY' AND game.status='PUBLISHED'
)) THEN 'invalid netplay game variant'
ELSE '' END
FROM netplay_rooms candidate
WHERE candidate.id=?`
