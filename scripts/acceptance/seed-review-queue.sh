#!/usr/bin/env bash
set -euo pipefail

database_path="${1:-}"
if [[ -z "$database_path" || ! -f "$database_path" ]]; then
  echo "usage: seed-review-queue.sh DATABASE" >&2
  exit 2
fi

bulk_ready_digest="142c6b7c4a5c0fa5cc12000ce0ab107f6a5906712a7f08e53000c87427a1eb2a"
bulk_ready_path="$(dirname "$database_path")/blobs/sha256/${bulk_ready_digest:0:2}/${bulk_ready_digest:2:2}/$bulk_ready_digest"
mkdir -p "$(dirname "$bulk_ready_path")"
printf 'retrom deterministic review bulk approval fixture\n' >"$bulk_ready_path"
if [[ "$(sha256sum "$bulk_ready_path" | cut -d' ' -f1)" != "$bulk_ready_digest" ]]; then
  echo "review bulk approval fixture digest mismatch" >&2
  exit 1
fi

sqlite3 -bail "$database_path" <<'SQL'
PRAGMA foreign_keys=ON;
BEGIN IMMEDIATE;
CREATE TEMP TABLE acceptance_base AS
WITH base_job AS (
  SELECT i.id AS item_id,i.import_job_id AS job_id
  FROM import_items i
  JOIN import_jobs j ON j.id=i.import_job_id
  WHERE i.state='PUBLISHED' AND j.platform_id='gba'
  ORDER BY i.updated_at_ms DESC,i.id DESC
  LIMIT 1
), base_payload AS (
  SELECT file.blob_id,blob.size_bytes
  FROM games game
  JOIN platform_instances platform ON platform.id=game.platform_instance_id
  JOIN game_files file ON file.game_id=game.id
  JOIN blobs blob ON blob.id=file.blob_id
  WHERE game.status='PUBLISHED' AND platform.platform_id='gba'
  ORDER BY game.updated_at_ms DESC,game.id DESC,file.sort_order,file.logical_name
  LIMIT 1
)
SELECT base_job.item_id,base_job.job_id,base_payload.blob_id,base_payload.size_bytes
FROM base_job CROSS JOIN base_payload;

-- Item 57 owns distinct CAS bytes so the stateful review test can leave exactly
-- one strict READY, non-duplicate candidate for the quick-approval acceptance.
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('65000000-0000-7000-8000-000000000057',
       '142c6b7c4a5c0fa5cc12000ce0ab107f6a5906712a7f08e53000c87427a1eb2a',50,
       '07f8202ba70c1f5013349c58949327b3','e0b7435f9e64b2e8413ec0e08393fab6b4fffaef',
       '44d50099','application/octet-stream',1786000100057);

INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,finalization_no,finalize_job_id,version,expires_at_ms,created_at_ms,updated_at_ms,unconsumed_pruned_at_ms,last_error_code)
SELECT '10000000-0000-7000-8000-000000000001','COMPLETE','FILES',2,size_bytes+50,
       '1000000000000000000000000000000000000000000000000000000000000001',1,NULL,1,
       4102444800000,1786000100000,1786000100000,NULL,NULL
FROM acceptance_base;
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,finalization_no,finalize_job_id,version,expires_at_ms,created_at_ms,updated_at_ms,unconsumed_pruned_at_ms,last_error_code)
SELECT '10000000-0000-7000-8000-000000000002','COMPLETE','FILES',1,size_bytes,
       '2000000000000000000000000000000000000000000000000000000000000002',1,NULL,1,
       4102444800000,1786000200000,1786000200000,NULL,NULL
FROM acceptance_base;

INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,payload_released_at_ms,last_error_code,created_at_ms,updated_at_ms)
SELECT '11000000-0000-7000-8000-000000000001','10000000-0000-7000-8000-000000000001',
       'shared/Game.gba',size_bytes,size_bytes,blob_id,'COMPLETE',NULL,NULL,1786000100000,1786000100000
FROM acceptance_base;
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,payload_released_at_ms,last_error_code,created_at_ms,updated_at_ms)
VALUES('11000000-0000-7000-8000-000000000057','10000000-0000-7000-8000-000000000001',
       'unique/Game-57.gba',50,50,'65000000-0000-7000-8000-000000000057','COMPLETE',NULL,NULL,
       1786000100057,1786000100057);
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,payload_released_at_ms,last_error_code,created_at_ms,updated_at_ms)
SELECT '11000000-0000-7000-8000-000000000002','10000000-0000-7000-8000-000000000002',
       'shared/Game.gba',size_bytes,size_bytes,blob_id,'COMPLETE',NULL,NULL,1786000200000,1786000200000
FROM acceptance_base;

INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,target_id,dat_version_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,queued_item_count,running_item_count,review_pending_item_count,published_item_count,discarded_item_count,failed_item_count,cancelled_item_count,ignored_file_count,rejected_file_count,last_error_code,cancel_requested_at_ms,cancel_reason,version,created_at_ms,updated_at_ms,completed_at_ms)
SELECT '20000000-0000-7000-8000-000000000001','10000000-0000-7000-8000-000000000001',target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,target_id,dat_version_id,'NONE',config_snapshot_json,config_snapshot_digest,'REVIEW_PENDING',60,0,0,60,0,0,0,0,0,0,NULL,NULL,NULL,1,1786000100000,1786000100000,NULL
FROM import_jobs WHERE id=(SELECT job_id FROM acceptance_base);
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,target_id,dat_version_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,queued_item_count,running_item_count,review_pending_item_count,published_item_count,discarded_item_count,failed_item_count,cancelled_item_count,ignored_file_count,rejected_file_count,last_error_code,cancel_requested_at_ms,cancel_reason,version,created_at_ms,updated_at_ms,completed_at_ms)
SELECT '20000000-0000-7000-8000-000000000002','10000000-0000-7000-8000-000000000002',target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,target_id,dat_version_id,'NONE',config_snapshot_json,config_snapshot_digest,'REVIEW_PENDING',3,0,0,3,0,0,0,0,0,0,NULL,NULL,NULL,1,1786000200000,1786000200000,NULL
FROM import_jobs WHERE id=(SELECT job_id FROM acceptance_base);

WITH RECURSIVE generated(batch,n,max_n,job_id) AS (
  SELECT 1,1,60,'20000000-0000-7000-8000-000000000001'
  UNION ALL SELECT batch,n+1,max_n,job_id FROM generated WHERE n<max_n
  UNION ALL SELECT 2,1,3,'20000000-0000-7000-8000-000000000002' FROM generated WHERE batch=1 AND n=max_n
)
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,failed_stage,last_error_code,version,created_at_ms,updated_at_ms,completed_at_ms)
SELECT printf('30000000-0000-7000-80%02d-%012d',batch,n),job_id,printf('%064x',n),'REVIEW_PENDING',
	       json_object('files',json_array(json_object(
	         'blobId',CASE WHEN batch=1 AND n=57 THEN '65000000-0000-7000-8000-000000000057'
	                       ELSE (SELECT blob_id FROM acceptance_base) END,
	         'logicalName',printf('batch-%d/Game-%02d.gba',batch,n),'role','CONTENT'))),
	       printf('%064x',batch*1000+n),
	       lower(printf('batch %d game %02d',batch,n)),NULL,NULL,1,1786000000000+batch*100000+n,1786000000000+batch*100000+n,NULL
FROM generated;

INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
SELECT i.id,'CONTENT',
       printf('batch-%d/Game-%02d.gba',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       CASE WHEN i.id='30000000-0000-7000-8001-000000000057' THEN '11000000-0000-7000-8000-000000000057'
            WHEN i.import_job_id LIKE '%1' THEN '11000000-0000-7000-8000-000000000001'
            ELSE '11000000-0000-7000-8000-000000000002' END,
       CASE WHEN i.id='30000000-0000-7000-8001-000000000057' THEN '65000000-0000-7000-8000-000000000057'
            ELSE (SELECT blob_id FROM acceptance_base) END,
       NULL,NULL,0,i.created_at_ms
FROM import_items i
WHERE i.id LIKE '30000000-%';

INSERT INTO import_item_source_snapshots(id,import_item_id,source_manifest_json,source_manifest_digest,created_by,created_at_ms)
SELECT printf('35000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       i.id,i.source_manifest_json,i.source_manifest_digest,'IDENTIFICATION',i.created_at_ms
FROM import_items i
WHERE i.id LIKE '30000000-%';

INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
SELECT printf('35000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       s.role,s.logical_name,s.upload_file_id,s.blob_id,s.source_archive_blob_id,s.source_archive_entry_ordinal,s.sort_order,s.created_at_ms
FROM import_items i
JOIN import_item_source_files s ON s.import_item_id=i.id
WHERE i.id LIKE '30000000-%';

-- The copied digest intentionally belongs to another source snapshot. This seeds
-- stale source evidence and verifies that draft save rebuilds it instead
-- of trusting only the structurally matching validation columns.
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms)
SELECT printf('40000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),i.id,v.target_platform_instance_id,v.platform_instance_version,v.core_id,v.provider_id,v.target_id,v.dat_version_id,v.default_dos_entry,i.source_manifest_digest,
       printf('35000000-0000-7000-80%02d-%012d',CASE WHEN i.import_job_id LIKE '%1' THEN 1 ELSE 2 END,CAST(substr(i.id,-12) AS INTEGER)),
       v.prepublish_input_digest,'READY','READY',v.dependency_snapshot_json,i.created_at_ms
FROM import_items i
CROSS JOIN import_item_core_validations v
WHERE v.import_item_id=(SELECT item_id FROM acceptance_base)
AND v.id=(SELECT selected_validation_id FROM review_drafts WHERE import_item_id=(SELECT item_id FROM acceptance_base))
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
