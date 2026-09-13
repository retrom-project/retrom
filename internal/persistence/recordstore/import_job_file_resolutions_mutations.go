package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/persistence/dbexec"
)

func UpdateImportJobFileResolutions(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_job_file_resolutions",
		"import_job_id,upload_file_id",
		ImportJobFileResolutionsUpdateRule,
	)
}

const ImportJobFileResolutionsUpdateRule = `
WITH previous(import_job_id,upload_file_id) AS (VALUES(?,?))
SELECT CASE
-- import_job_file_resolutions_immutable_update
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM import_job_file_resolutions candidate CROSS JOIN previous
WHERE candidate.import_job_id=previous.import_job_id AND candidate.upload_file_id=previous.upload_file_id`

func DeleteImportJobFileResolutions(
	ctx context.Context, db dbexec.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"import_job_file_resolutions",
		"import_job_id,upload_file_id",
		ImportJobFileResolutionsDeleteRule,
	)
}

const ImportJobFileResolutionsDeleteRule = `
WITH previous(import_job_id,upload_file_id) AS (VALUES(?,?))
SELECT CASE
-- import_job_file_resolutions_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
