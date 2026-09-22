package storequery

// DiscardedImportJobs is the shared relational projection used by application queries.
const DiscardedImportJobs = `
SELECT import_id FROM import_batch_discards WHERE kind='IMPORT'
UNION
SELECT item.library_import_job_id FROM source_import_items item
JOIN import_batch_discards batch ON batch.kind='SOURCE' AND batch.import_id=item.import_id
WHERE item.library_import_job_id IS NOT NULL
UNION
SELECT job.id FROM import_jobs job
JOIN server_import_upload_owners owner ON owner.upload_session_id=job.upload_session_id
JOIN source_import_items item ON owner.kind='SOURCE' AND item.id=owner.source_item_id
JOIN import_batch_discards batch ON batch.kind=owner.kind AND batch.import_id=item.import_id
`
