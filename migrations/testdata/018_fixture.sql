PRAGMA defer_foreign_keys=ON;

INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('v18-content','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaa','application/zip',1);

INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,
size_bytes,sha256,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES('v18-artifact','fbneo','4.2.3','fixture','WASM','data/cores/v18.data',1,
'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','{}',
'{"requestedArtifactBasename":"v18.data"}',1,1,1,1);

INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,sha256,parser_version,
compatibility_status,parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('v18-dat','fbneo','v18-artifact','BUILTIN','data/dat/v18.xml',
'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','fixture',
'MATCHED','READY',1,1,1,1,1,1);

INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES('v18-upload','COMPLETE','FILES',1,1,
'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',1,1000,1,1);
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_blob_id,state,created_at_ms,updated_at_ms)
VALUES('v18-file','v18-upload','a.zip',1,1,'v18-content','COMPLETE',1,1);

INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,core_artifact_id,dat_version_id,metadata_provider,config_snapshot_json,
config_snapshot_digest,state,total_item_count,review_pending_item_count,version,created_at_ms,updated_at_ms)
VALUES('v18-import','v18-upload','01980000-0000-7000-8000-000000000006',1,'arcade','fbneo',
'v18-artifact','v18-dat','NONE','{}',
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
'REVIEW_PENDING',1,1,1,1,1);
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,version,created_at_ms,updated_at_ms)
VALUES('v18-item','v18-import',
'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff','REVIEW_PENDING',
'[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"a.zip","role":"CONTENT","sizeBytes":1}]',
'0d33b3ade494963aac48ba52e5e86a9454504887c2a70740e0cc5ff37334478e','a.zip',1,1,1);
INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,blob_id,
source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES('v18-item','CONTENT','a.zip','v18-file','v18-content',NULL,NULL,0,1);
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,core_artifact_id,dat_version_id,source_manifest_digest,
prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES('v18-validation','v18-item','01980000-0000-7000-8000-000000000006',1,'fbneo',
'v18-artifact','v18-dat','0d33b3ade494963aac48ba52e5e86a9454504887c2a70740e0cc5ff37334478e',
'1111111111111111111111111111111111111111111111111111111111111111','BLOCKED',
'LAUNCH_PARENT_MISSING',
'{"schemaVersion":1,"machine":"a","datVersionId":"v18-dat","closure":["a"],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}',1);
INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,selected_validation_id,
metadata_json,version,created_at_ms,updated_at_ms)
VALUES('v18-draft','v18-item','01980000-0000-7000-8000-000000000006',NULL,
'{"title":"A","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}',1,1,1);

INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES('v18-job','IMPORT_ITEM','v18-item','METADATA_SCRAPE',
'2222222222222222222222222222222222222222222222222222222222222222',1,'{}',0,'SUCCEEDED',
1,2,1,1,1,1,1,1);
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES('v18-job',1,'{}','3333333333333333333333333333333333333333333333333333333333333333',1);
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES('v18-job','IMPORT_ITEM','v18-item','SUCCEEDED','{}',1);
INSERT INTO metadata_scrape_runs(id,import_item_id,job_id,provider,provider_config_version,state,
version,created_at_ms,updated_at_ms,completed_at_ms)
VALUES('v18-scrape','v18-item','v18-job','NONE',1,'COMPLETED',1,1,1,1);
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES('v18-consumption','v18-upload',NULL,'IMPORT_JOB','v18-import',1);
INSERT INTO review_events(id,import_item_id,event_type,actor,before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES('v18-event','v18-item','DRAFT_SAVED','local','{}','{}','{}','{}','{}','{}',1);
