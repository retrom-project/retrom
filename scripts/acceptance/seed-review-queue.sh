#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -f "$database_path" ]]; then
  echo "usage: seed-review-queue.sh DATABASE" >&2
  exit 2
fi

sqlite3 -bail "$database_path" <<'SQL'
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;
CREATE TEMP TABLE acceptance_base AS
SELECT i.id AS item_id,
       i.import_job_id AS job_id,
       j.upload_session_id AS upload_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
WHERE i.state='PUBLISHED'
ORDER BY i.updated_at_ms DESC,i.id DESC
LIMIT 1;

INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,finalization_no,finalize_job_id,version,expires_at_ms,created_at_ms,updated_at_ms,unconsumed_pruned_at_ms,last_error_code)
SELECT '10000000-0000-7000-8000-000000000001','COMPLETE',source_type,total_files,total_bytes,manifest_digest,finalization_no,NULL,1,expires_at_ms,1786000100000,1786000100000,NULL,NULL
FROM upload_sessions WHERE id=(SELECT upload_id FROM acceptance_base);
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,finalization_no,finalize_job_id,version,expires_at_ms,created_at_ms,updated_at_ms,unconsumed_pruned_at_ms,last_error_code)
SELECT '10000000-0000-7000-8000-000000000002','COMPLETE',source_type,total_files,total_bytes,manifest_digest,finalization_no,NULL,1,expires_at_ms,1786000200000,1786000200000,NULL,NULL
FROM upload_sessions WHERE id=(SELECT upload_id FROM acceptance_base);

INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,core_artifact_id,dat_version_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,queued_item_count,running_item_count,review_pending_item_count,published_item_count,discarded_item_count,failed_item_count,cancelled_item_count,ignored_file_count,rejected_file_count,last_error_code,cancel_requested_at_ms,cancel_reason,version,created_at_ms,updated_at_ms,completed_at_ms)
SELECT '20000000-0000-7000-8000-000000000001','10000000-0000-7000-8000-000000000001',target_platform_instance_id,platform_instance_version,platform_id,default_core_id,core_artifact_id,dat_version_id,'NONE',config_snapshot_json,config_snapshot_digest,'REVIEW_PENDING',60,0,0,60,0,0,0,0,0,0,NULL,NULL,NULL,1,1786000100000,1786000100000,NULL
FROM import_jobs WHERE id=(SELECT job_id FROM acceptance_base);
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,core_artifact_id,dat_version_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,queued_item_count,running_item_count,review_pending_item_count,published_item_count,discarded_item_count,failed_item_count,cancelled_item_count,ignored_file_count,rejected_file_count,last_error_code,cancel_requested_at_ms,cancel_reason,version,created_at_ms,updated_at_ms,completed_at_ms)
SELECT '20000000-0000-7000-8000-000000000002','10000000-0000-7000-8000-000000000002',target_platform_instance_id,platform_instance_version,platform_id,default_core_id,core_artifact_id,dat_version_id,'NONE',config_snapshot_json,config_snapshot_digest,'REVIEW_PENDING',3,0,0,3,0,0,0,0,0,0,NULL,NULL,NULL,1,1786000200000,1786000200000,NULL
FROM import_jobs WHERE id=(SELECT job_id FROM acceptance_base);

WITH RECURSIVE generated(batch,n,max_n,job_id) AS (
  SELECT 1,1,60,'20000000-0000-7000-8000-000000000001'
  UNION ALL SELECT batch,n+1,max_n,job_id FROM generated WHERE n<max_n
  UNION ALL SELECT 2,1,3,'20000000-0000-7000-8000-000000000002' FROM generated WHERE batch=1 AND n=max_n
)
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,failed_stage,last_error_code,version,created_at_ms,updated_at_ms,completed_at_ms)
SELECT printf('30000000-0000-7000-80%02d-%012d',batch,n),job_id,printf('%064x',n),'REVIEW_PENDING',
       json_object('files',json_array(json_object('blobId',(SELECT blob_id FROM import_item_source_files WHERE import_item_id=(SELECT item_id FROM acceptance_base) ORDER BY sort_order LIMIT 1),'logicalName',printf('batch-%d/Game-%02d.gba',batch,n),'role','CONTENT'))),
       (SELECT source_manifest_digest FROM import_items WHERE id=(SELECT item_id FROM acceptance_base)),
       lower(printf('batch %d game %02d',batch,n)),NULL,NULL,1,1786000000000+batch*100000+n,1786000000000+batch*100000+n,NULL
FROM generated;

INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
SELECT i.id,s.role,printf('batch-%d/Game-%02d.gba',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),s.upload_file_id,s.blob_id,s.source_archive_blob_id,s.source_archive_entry_ordinal,s.sort_order,i.created_at_ms
FROM import_items i
CROSS JOIN import_item_source_files s
WHERE s.import_item_id=(SELECT item_id FROM acceptance_base)
AND i.id LIKE '30000000-%';

INSERT INTO import_item_source_snapshots(id,import_item_id,revision_no,source_manifest_json,source_manifest_digest,created_by,created_at_ms)
SELECT printf('35000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       i.id,1,i.source_manifest_json,i.source_manifest_digest,'IDENTIFICATION',i.created_at_ms
FROM import_items i
WHERE i.id LIKE '30000000-%';

INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
SELECT printf('35000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       s.role,s.logical_name,s.upload_file_id,s.blob_id,s.source_archive_blob_id,s.source_archive_entry_ordinal,s.sort_order,s.created_at_ms
FROM import_items i
JOIN import_item_source_files s ON s.import_item_id=i.id
WHERE i.id LIKE '30000000-%';

INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,core_artifact_id,dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms)
SELECT printf('40000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),i.id,v.target_platform_instance_id,v.platform_instance_version,v.core_id,v.core_artifact_id,v.dat_version_id,v.default_dos_entry,i.source_manifest_digest,
       printf('35000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       v.prepublish_input_digest,'READY','READY',v.dependency_snapshot_json,i.created_at_ms
FROM import_items i
CROSS JOIN import_item_core_validations v
WHERE v.import_item_id=(SELECT item_id FROM acceptance_base)
AND v.id=(SELECT selected_validation_id FROM review_drafts WHERE import_item_id=(SELECT item_id FROM acceptance_base))
AND i.id LIKE '30000000-%';

INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms)
SELECT printf('40000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),f.role,f.logical_name,f.blob_id,f.sort_order,i.created_at_ms
FROM import_items i
CROSS JOIN import_item_validation_files f
WHERE f.import_item_core_validation_id=(SELECT selected_validation_id FROM review_drafts WHERE import_item_id=(SELECT item_id FROM acceptance_base))
AND i.id LIKE '30000000-%';

INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,effective_source_snapshot_id,selected_validation_id,selected_candidate_id,cover_candidate_asset_id,background_candidate_asset_id,default_dos_entry,metadata_json,version,created_at_ms,updated_at_ms)
SELECT printf('50000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),i.id,v.target_platform_instance_id,v.source_snapshot_id,v.id,NULL,NULL,NULL,v.default_dos_entry,
       json_object('title',printf('Batch %d Game %02d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),'description','','developer','','publisher','','genre','','players',NULL,'releaseYear',NULL),1,i.created_at_ms,i.updated_at_ms
FROM import_items i
JOIN import_item_core_validations v ON v.import_item_id=i.id
WHERE i.id LIKE '30000000-%';

DROP TABLE acceptance_base;
COMMIT;
SQL

count="$(sqlite3 "$database_path" "SELECT count(*) FROM import_items WHERE state='REVIEW_PENDING' AND id LIKE '30000000-%';")"
if [[ "$count" != "63" ]]; then
  echo "review queue seed count mismatch: $count" >&2
  exit 1
fi
printf 'primary_import_job_id=20000000-0000-7000-8000-000000000001\nsecondary_import_job_id=20000000-0000-7000-8000-000000000002\nreview_pending=%s\n' "$count"
