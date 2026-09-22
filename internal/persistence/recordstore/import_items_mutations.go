package recordstore

import (
	"context"
	"database/sql"

	"retrom/internal/dbexec"
)

func UpdateImportItems(
	ctx context.Context, db dbexec.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_items",
		"id,state",
		ImportItemsUpdateRule,
	)
}

const ImportItemsUpdateRule = `
WITH previous(id,state) AS (VALUES(?,?))
SELECT CASE
-- discarded_import_publication_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='PUBLISHED' AND EXISTS(SELECT 1 FROM
(SELECT import_id FROM import_batch_discards WHERE kind='IMPORT'
UNION
SELECT item.library_import_job_id FROM source_import_items item
JOIN import_batch_discards batch ON batch.kind='SOURCE' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON
owner.upload_session_id=job.upload_session_id
JOIN source_import_items item ON owner.kind='SOURCE' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id) WHERE
import_id=candidate.import_job_id))) THEN 'IMPORT_BATCH_DISCARDED'
-- discarded_import_retry_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='QUEUED' AND previous.state<>'QUEUED'
AND EXISTS(SELECT 1 FROM (SELECT import_id FROM import_batch_discards WHERE kind='IMPORT'
UNION
SELECT item.library_import_job_id FROM source_import_items item
JOIN import_batch_discards batch ON batch.kind='SOURCE' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON
owner.upload_session_id=job.upload_session_id
JOIN source_import_items item ON owner.kind='SOURCE' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id) WHERE
import_id=candidate.import_job_id))) THEN 'IMPORT_BATCH_DISCARDED'
ELSE '' END
FROM import_items candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
