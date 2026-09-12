package recordstore

import (
	"context"
	"database/sql"
)

func UpdateNetplaySessionParticipants(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"netplay_session_participants",
		"netplay_session_id,profile_id,credential_generation,credential_sha256,"+
			"launch_session_id,player_no,room_member_id",
		NetplaySessionParticipantsUpdateRule,
	)
}

const NetplaySessionParticipantsUpdateRule = `
WITH previous(netplay_session_id,profile_id,credential_generation,credential_sha256,launch_session_id,
player_no,room_member_id) AS (VALUES(?,?,?,?,?,?,?))
SELECT CASE
-- netplay_session_participants_immutable_identity
WHEN ((candidate.netplay_session_id IS NOT previous.netplay_session_id OR candidate.profile_id IS NOT
previous.profile_id OR candidate.room_member_id IS NOT previous.room_member_id OR candidate.player_no IS
NOT previous.player_no OR candidate.launch_session_id IS NOT previous.launch_session_id OR
candidate.credential_sha256 IS NOT previous.credential_sha256 OR candidate.credential_generation IS NOT
previous.credential_generation) AND (previous.launch_session_id IS NOT NULL)) THEN
'immutable netplay participant identity'
ELSE '' END
FROM netplay_session_participants candidate CROSS JOIN previous
WHERE candidate.netplay_session_id=previous.netplay_session_id AND
candidate.profile_id=previous.profile_id`
