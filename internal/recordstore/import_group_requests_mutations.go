package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportGroupRequests(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_group_requests",
		"import_job_id",
		ImportGroupRequestsUpdateRule,
	)
}

const ImportGroupRequestsUpdateRule = `
WITH previous(import_job_id) AS (VALUES(?))
SELECT CASE
-- import_group_requests_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_group_requests candidate CROSS JOIN previous
WHERE candidate.import_job_id=previous.import_job_id`

func DeleteImportGroupRequests(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_group_requests",
		"import_job_id",
		ImportGroupRequestsDeleteRule,
	)
}

const ImportGroupRequestsDeleteRule = `
WITH previous(import_job_id) AS (VALUES(?))
SELECT CASE
-- import_group_requests_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
