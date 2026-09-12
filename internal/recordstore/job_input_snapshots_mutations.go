package recordstore

import (
	"context"
	"database/sql"
)

func UpdateJobInputSnapshots(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"job_input_snapshots",
		"job_id,execution_no",
		JobInputSnapshotsUpdateRule,
	)
}

const JobInputSnapshotsUpdateRule = `
WITH previous(job_id,execution_no) AS (VALUES(?,?))
SELECT CASE
-- job_input_snapshots_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM job_input_snapshots candidate CROSS JOIN previous
WHERE candidate.job_id=previous.job_id AND candidate.execution_no=previous.execution_no`

func DeleteJobInputSnapshots(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"job_input_snapshots",
		"job_id,execution_no",
		JobInputSnapshotsDeleteRule,
	)
}

const JobInputSnapshotsDeleteRule = `
WITH previous(job_id,execution_no) AS (VALUES(?,?))
SELECT CASE
-- job_input_snapshots_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
