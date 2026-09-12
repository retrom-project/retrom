package recordstore

import (
	"context"
	"database/sql"
)

func UpdateNetplaySessions(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"netplay_sessions",
		"id,bundle_sha256,provider_id,target_id",
		NetplaySessionsUpdateRule,
	)
}

const NetplaySessionsUpdateRule = `
WITH previous(id,bundle_sha256,provider_id,target_id) AS (VALUES(?,?,?,?))
SELECT CASE
-- netplay_sessions_runtime_target_immutable
WHEN ((candidate.provider_id IS NOT previous.provider_id OR candidate.target_id IS NOT
previous.target_id OR candidate.bundle_sha256 IS NOT previous.bundle_sha256) AND (1=1)) THEN
'immutable runtime netplay snapshot'
ELSE '' END
FROM netplay_sessions candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
