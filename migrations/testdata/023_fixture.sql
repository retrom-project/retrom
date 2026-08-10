PRAGMA defer_foreign_keys=ON;
BEGIN;

INSERT INTO profiles(id,display_name,created_at_ms) VALUES
  ('01980000-0000-7000-8000-00000000a001','Migration Admin',1000),
  ('01980000-0000-7000-8000-00000000a002','Migration Player',1000);
INSERT INTO users(id,profile_id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms)
VALUES
  ('01980000-0000-7000-8000-00000000b001','01980000-0000-7000-8000-00000000a001',
   'migration.admin','Migration Admin','ADMIN','ENABLED',1,1,1000,1000),
  ('01980000-0000-7000-8000-00000000b002','01980000-0000-7000-8000-00000000a002',
   'migration.player','Migration Player','USER','ENABLED',1,1,1000,1000);
INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES
  ('01980000-0000-7000-8000-00000000b001','fixture-admin','ARGON2ID_V1',1000,1000),
  ('01980000-0000-7000-8000-00000000b002','fixture-player','ARGON2ID_V1',1000,1000);
UPDATE instance_state
SET state='COMPLETED',bootstrap_kind='RELEASE_SETUP',
    initial_admin_user_id='01980000-0000-7000-8000-00000000b001',
    version=2,updated_at_ms=1000,initialized_at_ms=1000
WHERE id=1;

INSERT INTO core_artifacts(
  id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,
  source_commit,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000c001','mgba','4.2.3','4.2.3','WASM',
  'data/cores/mgba-wasm.data',1055616,
  '01fcaf6d4296ef1db6676e0c69400c4474e24572d0b2b99cc097e4ae885e02d7',NULL,'{}',
  '{"schemaVersion":2,"runtimeCoreId":"mgba","requestedArtifactBasename":"mgba-wasm.data","canvasResizePolicy":"NONE","defaultOptions":{},"persistentSaveMode":"SINGLE_FILE","persistentSaveKind":"CORE_SAVE","inputMode":"STANDARD","startupActions":[]}',
  1,5,1000,1000
);

INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES
  ('01980000-0000-7000-8000-00000000d001','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',8,
   'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaa','application/octet-stream',1000),
  ('01980000-0000-7000-8000-00000000d002','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',8,
   'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','bbbbbbbb','application/octet-stream',1000),
  ('01980000-0000-7000-8000-00000000d003','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',8,
   'cccccccccccccccccccccccccccccccc','cccccccccccccccccccccccccccccccccccccccc','cccccccc','image/png',1000),
  ('01980000-0000-7000-8000-00000000d004','dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',8,
   'dddddddddddddddddddddddddddddddd','dddddddddddddddddddddddddddddddddddddddd','dddddddd','application/octet-stream',1000);

INSERT INTO dat_versions(
  id,core_id,core_artifact_id,source,builtin_relative_path,blob_id,sha256,parser_version,
  compatibility_status,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,
  bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,
  unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f009','mgba','01980000-0000-7000-8000-00000000c001',
  'BUILTIN','fixture.dat',NULL,'7777777777777777777777777777777777777777777777777777777777777777',
  'fixture-v1','MATCHED','READY',0,0,0,0,0,0,0,0,0,1,1000,1000,1000
);

INSERT INTO upload_sessions(
  id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000e001','COMPLETE','FILES',1,8,
  'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',1,100000,1000,1000
);
INSERT INTO upload_files(
  id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000e001',
  'fixture.gba',8,8,'01980000-0000-7000-8000-00000000d001','COMPLETE',1000,1000
);
INSERT INTO import_jobs(
  id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
  core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
  review_pending_item_count,version,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f001','01980000-0000-7000-8000-00000000e001',
  '01980000-0000-7000-8000-000000000005',1,'gba','mgba','01980000-0000-7000-8000-00000000c001',
  'NONE','{"schemaVersion":1}','ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
  'REVIEW_PENDING',1,1,1,1000,1000
);
INSERT INTO import_items(
  id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,version,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f002','01980000-0000-7000-8000-00000000f001',
  '1111111111111111111111111111111111111111111111111111111111111111','REVIEW_PENDING',
  '[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"fixture.gba","role":"CONTENT","sizeBytes":8}]',
  '2222222222222222222222222222222222222222222222222222222222222222','fixture',1,1000,1000
);
INSERT INTO import_item_source_files(
  import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f002','CONTENT','fixture.gba',
  '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001',NULL,NULL,0,1000
);
INSERT INTO import_item_source_snapshots(
  id,import_item_id,revision_no,source_manifest_json,source_manifest_digest,created_by,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f003','01980000-0000-7000-8000-00000000f002',1,
  '[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"fixture.gba","role":"CONTENT","sizeBytes":8}]',
  '2222222222222222222222222222222222222222222222222222222222222222','IDENTIFICATION',1000
);
INSERT INTO import_item_source_snapshot_files(
  source_snapshot_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f003','CONTENT','fixture.gba',
  '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001',NULL,NULL,0,1000
);
INSERT INTO import_item_source_snapshots(
  id,import_item_id,revision_no,source_manifest_json,source_manifest_digest,created_by,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f030','01980000-0000-7000-8000-00000000f002',2,
  '[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"fixture.zip","role":"DOS_SOURCE","sizeBytes":8}]',
  '8888888888888888888888888888888888888888888888888888888888888888','IDENTIFICATION',1000
);
INSERT INTO import_item_source_snapshot_files(
  source_snapshot_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f030','DOS_SOURCE','fixture.zip',
  '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001',NULL,NULL,0,1000
);
INSERT INTO import_item_core_validations(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,core_artifact_id,
  dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
  status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f004','01980000-0000-7000-8000-00000000f002',
  '01980000-0000-7000-8000-000000000005',1,'mgba','01980000-0000-7000-8000-00000000c001',
  NULL,NULL,'2222222222222222222222222222222222222222222222222222222222222222',
  '01980000-0000-7000-8000-00000000f003','3333333333333333333333333333333333333333333333333333333333333333',
  'READY','READY','{"schemaVersion":1,"validatorVersion":"review-compatible-v3"}',1000
);
INSERT INTO review_drafts(
  id,import_item_id,target_platform_instance_id,selected_validation_id,effective_source_snapshot_id,
  metadata_json,version,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f005','01980000-0000-7000-8000-00000000f002',
  '01980000-0000-7000-8000-000000000005','01980000-0000-7000-8000-00000000f004',
  '01980000-0000-7000-8000-00000000f003',
  '{"title":"Fixture","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}',
  7,1000,1000
);
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,after_json,diff_json,
  config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f006','01980000-0000-7000-8000-00000000f002','DRAFT_SAVED',
  'USER','01980000-0000-7000-8000-00000000b001',NULL,'{}','{}','{}','{}','{}','{}',NULL,1000
);

INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,
  version,available_at_ms,execution_started_at_ms,finished_at_ms,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f007','IMPORT_ITEM','01980000-0000-7000-8000-00000000f002',
  'REVIEW_ARCADE_PARENT_VALIDATE','4444444444444444444444444444444444444444444444444444444444444444',
  1,'{"schemaVersion":1,"inputExecutionNo":1}',1,'SUCCEEDED',1,4,1,1000,1000,1000,1000,1000
);
INSERT INTO review_arcade_parent_attachments(
  id,import_item_id,review_draft_id,base_source_snapshot_id,result_source_snapshot_id,
  dependency_machine,expected_logical_name,required_by_machine,depth,core_artifact_id,dat_version_id,
  upload_file_id,accepted_blob_id,original_filename,observed_size_bytes,observed_sha256,state,error_code,
  diagnostics_json,job_id,version,created_at_ms,updated_at_ms,finished_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f008','01980000-0000-7000-8000-00000000f002',
  '01980000-0000-7000-8000-00000000f005','01980000-0000-7000-8000-00000000f003',NULL,
  'parent','parent.zip','fixture',1,'01980000-0000-7000-8000-00000000c001',
  '01980000-0000-7000-8000-00000000f009',NULL,NULL,'parent.zip',8,
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','REJECTED','FIXTURE_REJECTED','{}',
  '01980000-0000-7000-8000-00000000f007',2,1000,1000,1000
);

INSERT INTO idempotency_records(
  principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,created_at_ms,expires_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000b001','fixture-operation','fixture-key',
  '5555555555555555555555555555555555555555555555555555555555555555',200,'{}',x'7b7d',1000,2000
);

INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f011','01980000-0000-7000-8000-00000000f010',
  'Fixture','','','','',NULL,NULL,'IMPORT_REVIEW','01980000-0000-7000-8000-00000000f002',1000
);
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f012','01980000-0000-7000-8000-00000000f010',
  'IMPORT_REVIEW','01980000-0000-7000-8000-00000000f002',
  '[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"fixture.gba","role":"CONTENT","sizeBytes":8}]',
  '2222222222222222222222222222222222222222222222222222222222222222',1000
);
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f010','01980000-0000-7000-8000-000000000005','PUBLISHED',
  '01980000-0000-7000-8000-00000000f011','01980000-0000-7000-8000-00000000f012','fixture',1,1000,1000
);
INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order)
VALUES('01980000-0000-7000-8000-00000000f012','CONTENT','fixture.gba','01980000-0000-7000-8000-00000000d001',0);
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f015','01980000-0000-7000-8000-00000000f010',
  'ADMIN_REPLACE','fixture-replacement',
  '[{"blobSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","logicalName":"fixture.zip","role":"DOS_SOURCE","sizeBytes":8}]',
  '9999999999999999999999999999999999999999999999999999999999999999',1000
);
INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order)
VALUES('01980000-0000-7000-8000-00000000f015','DOS_SOURCE','fixture.zip','01980000-0000-7000-8000-00000000d001',0);
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000f013','01980000-0000-7000-8000-00000000f010','mgba',NULL,1,1000,1000);
INSERT INTO game_variant_revisions(
  id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,validation_input_digest,
  emulator_game_id,status,compatibility_code,dependency_snapshot_json,default_dos_entry,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f014','01980000-0000-7000-8000-00000000f013',
  '01980000-0000-7000-8000-00000000f012','01980000-0000-7000-8000-00000000c001',NULL,
  '6666666666666666666666666666666666666666666666666666666666666666',6001,'READY','READY','{}',NULL,1000
);
UPDATE game_variants SET current_revision_id='01980000-0000-7000-8000-00000000f014'
WHERE id='01980000-0000-7000-8000-00000000f013';

INSERT INTO launch_sessions(
  id,profile_id,game_id,game_variant_revision_id,core_artifact_id,return_to,credential_sha256,state,
  bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version
) VALUES
  ('01980000-0000-7000-8000-00000000f021','01980000-0000-7000-8000-00000000a001',
   '01980000-0000-7000-8000-00000000f010','01980000-0000-7000-8000-00000000f014',
   '01980000-0000-7000-8000-00000000c001','/',zeroblob(32),'FINISHED',2000,1500,3000,1000,1500,1),
  ('01980000-0000-7000-8000-00000000f022','01980000-0000-7000-8000-00000000a002',
   '01980000-0000-7000-8000-00000000f010','01980000-0000-7000-8000-00000000f014',
   '01980000-0000-7000-8000-00000000c001','/',x'0101010101010101010101010101010101010101010101010101010101010101',
   'FINISHED',2000,1500,3000,1000,1500,1);
INSERT INTO save_states(
  id,profile_id,game_id,game_variant_revision_id,core_artifact_id,dat_version_id,dos_entry_path,
  state_blob_id,screenshot_blob_id,source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms
) VALUES
  ('01980000-0000-7000-8000-00000000f023','01980000-0000-7000-8000-00000000a001',
   '01980000-0000-7000-8000-00000000f010','01980000-0000-7000-8000-00000000f014',
   '01980000-0000-7000-8000-00000000c001',NULL,NULL,'01980000-0000-7000-8000-00000000d002',
   '01980000-0000-7000-8000-00000000d003','01980000-0000-7000-8000-00000000f021','Admin Save',10,1,1000,1000),
  ('01980000-0000-7000-8000-00000000f024','01980000-0000-7000-8000-00000000a002',
   '01980000-0000-7000-8000-00000000f010','01980000-0000-7000-8000-00000000f014',
   '01980000-0000-7000-8000-00000000c001',NULL,NULL,'01980000-0000-7000-8000-00000000d002',
   '01980000-0000-7000-8000-00000000d003','01980000-0000-7000-8000-00000000f022','Player Save',10,1,1000,1000);

INSERT INTO persistent_saves(
  id,profile_id,game_variant_revision_id,kind,current_revision_id,version,created_at_ms,updated_at_ms
) VALUES
  ('01980000-0000-7000-8000-00000000f025','01980000-0000-7000-8000-00000000a001',
   '01980000-0000-7000-8000-00000000f014','CORE_SAVE','01980000-0000-7000-8000-00000000f027',1,1000,1000),
  ('01980000-0000-7000-8000-00000000f026','01980000-0000-7000-8000-00000000a002',
   '01980000-0000-7000-8000-00000000f014','CORE_SAVE','01980000-0000-7000-8000-00000000f028',1,1000,1000);
INSERT INTO persistent_save_revisions(
  id,persistent_save_id,blob_id,source_launch_session_id,client_sequence,source_event,created_at_ms
) VALUES
  ('01980000-0000-7000-8000-00000000f027','01980000-0000-7000-8000-00000000f025',
   '01980000-0000-7000-8000-00000000d004','01980000-0000-7000-8000-00000000f021',1,'EXIT',1000),
  ('01980000-0000-7000-8000-00000000f028','01980000-0000-7000-8000-00000000f026',
   '01980000-0000-7000-8000-00000000d004','01980000-0000-7000-8000-00000000f022',1,'EXIT',1000);

COMMIT;
