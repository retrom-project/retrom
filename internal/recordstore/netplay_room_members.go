package recordstore

import (
	"context"
	"database/sql"
)

func CreateNetplayRoomMembers(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateNetplayRoomMembers)
}

func ValidateNetplayRoomMembers(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, netplay_room_membersOwnership, keys)
}

const netplay_room_membersOwnership = `
SELECT CASE
WHEN (candidate.role='HOST' AND NOT EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=candidate.room_id AND
room.host_profile_id=candidate.profile_id
) OR candidate.role='GUEST' AND EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=candidate.room_id AND
room.host_profile_id=candidate.profile_id
) OR candidate.ready=1 AND NOT EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=candidate.room_id AND room.state='WAITING'
)) THEN 'invalid netplay room member'
ELSE '' END
FROM netplay_room_members candidate
WHERE candidate.id=?`
