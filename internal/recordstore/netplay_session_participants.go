package recordstore

import (
	"context"
	"database/sql"
)

func CreateNetplaySessionParticipants(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "netplay_session_id,profile_id", ValidateNetplaySessionParticipants)
}

func ValidateNetplaySessionParticipants(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, netplay_session_participantsOwnership, keys)
}

const netplay_session_participantsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM netplay_sessions session
  JOIN netplay_room_members member ON member.room_id=session.room_id
  WHERE session.id=candidate.netplay_session_id AND member.id=candidate.room_member_id
    AND member.profile_id=candidate.profile_id AND member.player_no=candidate.player_no AND
member.left_at_ms IS NULL
)) THEN 'invalid netplay participant snapshot'
ELSE '' END
FROM netplay_session_participants candidate
WHERE candidate.netplay_session_id=? AND candidate.profile_id=?`
