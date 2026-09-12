package recordstore

import (
	"context"
	"database/sql"
)

func UpdateNetplayRooms(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"netplay_rooms",
		"id,created_at_ms,current_session_id,host_profile_id,max_players,netplay_profile_id,"+
			"profile_digest,selected_game_id,selected_game_variant_id,state",
		NetplayRoomsUpdateRule,
	)
}

const NetplayRoomsUpdateRule = `
WITH previous(id,created_at_ms,current_session_id,host_profile_id,max_players,netplay_profile_id,
profile_digest,selected_game_id,selected_game_variant_id,state) AS (VALUES(?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- netplay_rooms_current_session_fk_update
WHEN ((candidate.current_session_id IS NOT previous.current_session_id) AND
(candidate.current_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_sessions session WHERE session.id=candidate.current_session_id AND
session.room_id=candidate.id
))) THEN 'invalid netplay current session'
-- netplay_rooms_current_session_immutable
WHEN ((candidate.current_session_id IS NOT previous.current_session_id) AND (previous.current_session_id
IS NOT NULL AND candidate.current_session_id IS NOT previous.current_session_id
  AND candidate.state IN ('STARTING','RUNNING'))) THEN 'locked netplay room session'
-- netplay_rooms_game_variant_update
WHEN ((candidate.selected_game_id IS NOT previous.selected_game_id OR candidate.selected_game_variant_id
IS NOT previous.selected_game_variant_id) AND (candidate.selected_game_variant_id IS NOT NULL AND NOT
EXISTS(
  SELECT 1
  FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=candidate.selected_game_variant_id AND variant.game_id=candidate.selected_game_id
    AND variant.status='READY' AND game.status='PUBLISHED'
))) THEN 'invalid netplay game variant'
-- netplay_rooms_host_immutable
WHEN ((candidate.host_profile_id IS NOT previous.host_profile_id OR candidate.created_at_ms IS NOT
previous.created_at_ms) AND (1=1)) THEN 'immutable netplay room identity'
-- netplay_rooms_snapshot_immutable
WHEN ((candidate.selected_game_id IS NOT previous.selected_game_id OR candidate.selected_game_variant_id
IS NOT previous.selected_game_variant_id OR candidate.netplay_profile_id IS NOT
previous.netplay_profile_id OR candidate.profile_digest IS NOT previous.profile_digest OR
candidate.max_players IS NOT previous.max_players) AND (previous.state IN ('STARTING','RUNNING'))) THEN
'locked netplay room snapshot'
WHEN (candidate.state IN ('DRAFT','WAITING','STARTING','RUNNING') AND NOT EXISTS(
 SELECT 1 FROM netplay_room_members member WHERE member.room_id=candidate.id
 AND member.profile_id=candidate.host_profile_id AND member.role='HOST'
 AND member.player_no=1 AND member.left_at_ms IS NULL
)) THEN 'active netplay room requires host'
ELSE '' END
FROM netplay_rooms candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
