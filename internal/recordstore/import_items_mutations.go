package recordstore

import (
	"context"
	"database/sql"
)

func UpdateImportItems(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"import_items",
		"id,review_handoff_kind,state",
		ImportItemsUpdateRule,
	)
}

const ImportItemsUpdateRule = `
WITH previous(id,review_handoff_kind,state) AS (VALUES(?,?,?))
SELECT CASE
-- import_items_review_handoff_kind_immutable
WHEN ((candidate.review_handoff_kind IS NOT previous.review_handoff_kind) AND
(candidate.review_handoff_kind<>previous.review_handoff_kind)) THEN
'immutable import review handoff kind'
-- discarded_import_publication_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='PUBLISHED' AND EXISTS(SELECT 1 FROM
(SELECT import_id FROM import_batch_discards WHERE kind='IMPORT'
UNION
SELECT item.library_import_job_id FROM pegasus_import_items item
JOIN import_batch_discards batch ON batch.kind='PEGASUS' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT item.library_import_job_id FROM emulationstation_import_items item
JOIN import_batch_discards batch ON batch.kind='EMULATIONSTATION' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON
owner.upload_session_id=job.upload_session_id
JOIN pegasus_import_items item ON owner.kind='PEGASUS' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON
owner.upload_session_id=job.upload_session_id
JOIN emulationstation_import_items item ON owner.kind='EMULATIONSTATION' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id) WHERE
import_id=candidate.import_job_id))) THEN 'IMPORT_BATCH_DISCARDED'
-- discarded_import_retry_fence
WHEN ((candidate.state IS NOT previous.state) AND (candidate.state='QUEUED' AND previous.state<>'QUEUED'
AND EXISTS(SELECT 1 FROM (SELECT import_id FROM import_batch_discards WHERE kind='IMPORT'
UNION
SELECT item.library_import_job_id FROM pegasus_import_items item
JOIN import_batch_discards batch ON batch.kind='PEGASUS' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT item.library_import_job_id FROM emulationstation_import_items item
JOIN import_batch_discards batch ON batch.kind='EMULATIONSTATION' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON
owner.upload_session_id=job.upload_session_id
JOIN pegasus_import_items item ON owner.kind='PEGASUS' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id
UNION
SELECT job.id FROM import_jobs job JOIN server_import_upload_owners owner ON
owner.upload_session_id=job.upload_session_id
JOIN emulationstation_import_items item ON owner.kind='EMULATIONSTATION' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id) WHERE
import_id=candidate.import_job_id))) THEN 'IMPORT_BATCH_DISCARDED'
ELSE '' END
FROM import_items candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
