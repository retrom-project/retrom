package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateLaunchSessions(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"launch_sessions",
		"id,bundle_sha256,provider_id,"+
			"target_id",
		LaunchSessionsUpdateRule,
	)
}

const LaunchSessionsUpdateRule = `
WITH previous(id,bundle_sha256,provider_id,target_id)
AS (VALUES(?,?,?,?))
SELECT CASE
-- launch_sessions_runtime_target_immutable
WHEN ((candidate.provider_id IS NOT previous.provider_id OR candidate.target_id IS NOT
previous.target_id OR candidate.bundle_sha256 IS NOT previous.bundle_sha256) AND (1=1)) THEN
'immutable runtime target snapshot'
ELSE '' END
FROM launch_sessions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
