package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateLaunchSessions(
	ctx context.Context, db dbapi.Executor, change Update,
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
AS (VALUES(?::text,?::text,?::text,?::text))
SELECT CASE
-- launch_sessions_runtime_target_immutable
WHEN ((candidate.provider_id IS DISTINCT FROM previous.provider_id OR candidate.target_id IS DISTINCT FROM
 previous.target_id OR candidate.bundle_sha256 IS DISTINCT FROM previous.bundle_sha256) AND (1=1)) THEN
'immutable runtime target snapshot'
ELSE '' END
FROM launch_sessions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
