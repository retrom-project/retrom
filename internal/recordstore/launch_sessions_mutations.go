package recordstore

import (
	"context"
	"database/sql"
)

func UpdateLaunchSessions(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"launch_sessions",
		"id,bundle_sha256,netplay_player_no,netplay_session_id,provider_id,save_access,"+
			"target_id",
		LaunchSessionsUpdateRule,
	)
}

const LaunchSessionsUpdateRule = `
WITH previous(id,bundle_sha256,netplay_player_no,netplay_session_id,provider_id,save_access,target_id)
AS (VALUES(?,?,?,?,?,?,?))
SELECT CASE
-- launch_sessions_netplay_immutable
WHEN ((candidate.netplay_session_id IS NOT previous.netplay_session_id OR candidate.netplay_player_no IS
NOT previous.netplay_player_no OR candidate.save_access IS NOT previous.save_access) AND (1=1)) THEN
'immutable netplay launch binding'
-- launch_sessions_runtime_target_immutable
WHEN ((candidate.provider_id IS NOT previous.provider_id OR candidate.target_id IS NOT
previous.target_id OR candidate.bundle_sha256 IS NOT previous.bundle_sha256) AND (1=1)) THEN
'immutable runtime target snapshot'
ELSE '' END
FROM launch_sessions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
