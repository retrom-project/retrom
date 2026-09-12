package recordstore

import (
	"context"
	"database/sql"
)

func UpdateLaunchExternalFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"launch_external_files",
		"launch_session_id,virtual_path",
		LaunchExternalFilesUpdateRule,
	)
}

const LaunchExternalFilesUpdateRule = `
WITH previous(launch_session_id,virtual_path) AS (VALUES(?,?))
SELECT CASE
-- launch_external_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM launch_external_files candidate CROSS JOIN previous
WHERE candidate.launch_session_id=previous.launch_session_id AND
candidate.virtual_path=previous.virtual_path`

func DeleteLaunchExternalFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"launch_external_files",
		"launch_session_id,virtual_path",
		LaunchExternalFilesDeleteRule,
	)
}

const LaunchExternalFilesDeleteRule = `
WITH previous(launch_session_id,virtual_path) AS (VALUES(?,?))
SELECT CASE
-- launch_external_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  WHERE launch.id=previous.launch_session_id AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
)) THEN 'immutable'
ELSE '' END
FROM previous`
