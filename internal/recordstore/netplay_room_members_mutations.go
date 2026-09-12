package recordstore

import (
	"context"
	"database/sql"
)

func UpdateNetplayRoomMembers(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"netplay_room_members",
		"id,profile_id,role,room_id",
		NetplayRoomMembersUpdateRule,
	)
}

const NetplayRoomMembersUpdateRule = `
WITH previous(id,profile_id,role,room_id) AS (VALUES(?,?,?,?))
SELECT CASE
-- netplay_room_members_validate_update
WHEN (candidate.room_id!=previous.room_id OR candidate.profile_id!=previous.profile_id OR
candidate.role!=previous.role OR
  candidate.role='HOST' AND candidate.player_no!=1 OR candidate.ready=1 AND (
    candidate.left_at_ms IS NOT NULL OR NOT EXISTS(
      SELECT 1 FROM netplay_rooms room WHERE room.id=candidate.room_id AND room.state='WAITING'
    )
  )) THEN 'invalid netplay room member update'
ELSE '' END
FROM netplay_room_members candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
