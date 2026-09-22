package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateSourceImports(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"source_imports",
		"id,import_job_id,state",
		SourceImportsUpdateRule,
	)
}

const SourceImportsUpdateRule = `
WITH previous(id,import_job_id,state) AS (VALUES(?,?,?))
SELECT CASE
-- source_import_job_update
WHEN ((candidate.import_job_id IS NOT previous.import_job_id) AND (candidate.import_job_id IS NOT NULL
AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.import_job_id AND job.kind='IMPORT_RECEIVE'
  AND job.scope_type='SOURCE_IMPORT' AND job.scope_id=candidate.id
))) THEN 'invalid Source import job'
-- discarded_source_retry_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='QUEUED' AND EXISTS(
  SELECT 1 FROM import_batch_discards WHERE kind='SOURCE' AND import_id=candidate.id
))) THEN 'IMPORT_BATCH_DISCARDED'
ELSE '' END
FROM source_imports candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
