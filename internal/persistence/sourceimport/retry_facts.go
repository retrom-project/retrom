package sourceimport

const retryableItemCondition = "retryable=1 AND execution_state IN ('READ_FAILED','COMMIT_FAILED')"

const retrySummaryProjection = `,
 COALESCE(retry_job.state,''),COALESCE(retry_job.version,0),COALESCE(retry_job.execution_no,0),
 CASE WHEN import.state IN ('FAILED','PARTIAL_FAILURE') AND import.import_job_id IS NOT NULL
 THEN (SELECT count(*) FROM source_import_items WHERE import_id=import.id AND ` + retryableItemCondition + `)
 ELSE 0 END`

const retrySummaryJoin = ` LEFT JOIN jobs retry_job ON retry_job.id=import.import_job_id
 AND retry_job.scope_type='SOURCE_IMPORT' AND retry_job.scope_id=import.id AND retry_job.kind='IMPORT_RECEIVE'`
