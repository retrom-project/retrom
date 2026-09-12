package recordstore

import (
	"context"
	"database/sql"
)

func UpdatePegasusImports(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"pegasus_imports",
		"id,import_job_id,state",
		PegasusImportsUpdateRule,
	)
}

const PegasusImportsUpdateRule = `
WITH previous(id,import_job_id,state) AS (VALUES(?,?,?))
SELECT CASE
-- pegasus_import_job_update
WHEN ((candidate.import_job_id IS NOT previous.import_job_id) AND (candidate.import_job_id IS NOT NULL
AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=candidate.import_job_id AND job.kind='SERVER_PEGASUS_IMPORT'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=candidate.id
))) THEN 'invalid Pegasus import job'
-- discarded_pegasus_retry_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='QUEUED' AND EXISTS(
  SELECT 1 FROM import_batch_discards WHERE kind='PEGASUS' AND import_id=candidate.id
))) THEN 'IMPORT_BATCH_DISCARDED'
ELSE '' END
FROM pegasus_imports candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
