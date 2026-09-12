package recordstore

import (
	"context"
	"database/sql"
)

func UpdateLaunchContentFiles(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"launch_content_files",
		"launch_session_id,logical_name",
		LaunchContentFilesUpdateRule,
	)
}

const LaunchContentFilesUpdateRule = `
WITH previous(launch_session_id,logical_name) AS (VALUES(?,?))
SELECT CASE
-- launch_content_files_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM launch_content_files candidate CROSS JOIN previous
WHERE candidate.launch_session_id=previous.launch_session_id AND
candidate.logical_name=previous.logical_name`

func DeleteLaunchContentFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"launch_content_files",
		"launch_session_id,logical_name",
		LaunchContentFilesDeleteRule,
	)
}

const LaunchContentFilesDeleteRule = `
WITH previous(launch_session_id,logical_name) AS (VALUES(?,?))
SELECT CASE
-- launch_content_files_immutable_delete
WHEN (NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  WHERE launch.id=previous.launch_session_id AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
)) THEN 'immutable'
ELSE '' END
FROM previous`
