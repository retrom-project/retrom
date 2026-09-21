package payloadrelease

const lifecycleOwnersQuery = `WITH owners AS (
SELECT 'IMPORT_ITEM' AS scope_type,id,state,version,payload_state,
COALESCE(payload_release_job_id,'') AS release_job_id,'' AS public_id,0 AS retryable
FROM import_items
UNION ALL
SELECT 'IMPORT_JOB',id,state,version,payload_state,COALESCE(payload_release_job_id,''),'',0 FROM import_jobs
UNION ALL
SELECT 'GAME',id,status,version,payload_state,COALESCE(payload_release_job_id,''),'',0 FROM games
UNION ALL
SELECT 'PEGASUS_IMPORT_ITEM',id,execution_state,version,payload_state,
COALESCE(payload_release_job_id,''),COALESCE(library_import_item_id,''),retryable FROM pegasus_import_items
UNION ALL
SELECT 'EMULATIONSTATION_IMPORT_ITEM',id,execution_state,version,payload_state,
COALESCE(payload_release_job_id,''),COALESCE(library_import_item_id,''),retryable FROM emulationstation_import_items
)
SELECT owner.scope_type,owner.id,owner.state,owner.version,owner.payload_state,owner.release_job_id,
owner.public_id,owner.retryable,COALESCE(job.id,''),COALESCE(job.kind,''),COALESCE(job.scope_type,''),
COALESCE(job.scope_id,''),COALESCE(public.payload_release_job_id,'')
FROM owners owner LEFT JOIN jobs job ON job.id=owner.release_job_id
LEFT JOIN import_items public ON public.id=owner.public_id
WHERE owner.scope_type>? OR (owner.scope_type=? AND owner.id>?)
ORDER BY owner.scope_type,owner.id LIMIT ?`
