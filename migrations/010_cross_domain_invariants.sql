-- Final indexes and cross-domain invariants for the clean pre-release baseline.

CREATE INDEX account_links_creator ON account_links(created_by_user_id,kind,created_at_ms DESC);

CREATE INDEX account_links_kind_created ON account_links(kind,created_at_ms DESC,id DESC);

CREATE INDEX account_links_target ON account_links(target_user_id,kind,created_at_ms DESC);

CREATE INDEX audit_events_actor ON audit_events(actor_user_id,created_at_ms,id);

CREATE INDEX audit_events_resource ON audit_events(resource_type,resource_id,created_at_ms,id);

CREATE INDEX auth_sessions_expiry ON auth_sessions(absolute_expires_at_ms,revoked_at_ms);

CREATE INDEX auth_sessions_user_active ON auth_sessions(user_id,revoked_at_ms,absolute_expires_at_ms);

CREATE UNIQUE INDEX bios_installations_active ON bios_installations(requirement_id) WHERE is_active = 1;

CREATE UNIQUE INDEX bios_installations_server_candidate ON bios_installations(server_import_candidate_id)
WHERE server_import_candidate_id IS NOT NULL;

CREATE UNIQUE INDEX core_artifacts_selected_route
ON core_artifacts(core_id,route_key) WHERE selected_for_new_bindings=1;

CREATE UNIQUE INDEX core_artifacts_selected_rpgmaker_core
ON core_artifacts(core_id) WHERE selected_for_new_bindings=1 AND runtime_family='RPGMAKER';

CREATE UNIQUE INDEX dat_bios_sets_one_default ON dat_bios_sets(dat_version_id, machine_name) WHERE is_default = 1;

CREATE INDEX dat_rom_entries_crc32 ON dat_rom_entries(dat_version_id, crc32) WHERE crc32 IS NOT NULL;

CREATE INDEX dat_rom_entries_sha1 ON dat_rom_entries(dat_version_id, sha1) WHERE sha1 IS NOT NULL;

CREATE UNIQUE INDEX dat_versions_active ON dat_versions(core_artifact_id) WHERE is_active = 1;

CREATE UNIQUE INDEX dat_versions_bytes ON dat_versions(core_artifact_id, sha256, parser_version);

CREATE INDEX favorite_folder_games_folder
ON favorite_folder_games(profile_id,folder_id,created_at_ms,game_id);

CREATE INDEX favorite_folder_games_game
ON favorite_folder_games(profile_id,game_id,folder_id);

CREATE INDEX favorite_folders_profile_created
ON favorite_folders(profile_id,created_at_ms,id);

CREATE INDEX favorite_games_game
ON favorite_games(game_id,profile_id);

CREATE INDEX favorite_games_profile_created
ON favorite_games(profile_id,created_at_ms DESC,game_id DESC);

CREATE INDEX fk_archive_entries_materialized ON archive_entries(materialized_blob_id);

CREATE INDEX fk_bios_installations_blob ON bios_installations(blob_id);

CREATE INDEX fk_core_artifacts_core ON core_artifacts(core_id);

CREATE INDEX fk_runtime_asset_pack_files_blob ON runtime_asset_pack_files(blob_id);

CREATE INDEX fk_runtime_asset_pack_installations_bundle ON runtime_asset_pack_installations(bundle_blob_id);

CREATE INDEX fk_runtime_asset_pack_installations_definition
ON runtime_asset_pack_installations(definition_id,status,created_at_ms,id);

CREATE INDEX fk_game_variant_runtime_packs_installation
ON game_variant_revision_runtime_packs(installation_id,game_variant_revision_id);

CREATE INDEX fk_game_content_game ON game_content_revisions(game_id);

CREATE INDEX fk_game_metadata_game ON game_metadata_revisions(game_id);

CREATE INDEX fk_import_item_duplicate_matches_content_revision
ON import_item_duplicate_matches(existing_game_content_revision_id);

CREATE INDEX fk_import_item_duplicate_matches_game
ON import_item_duplicate_matches(existing_game_id);

CREATE INDEX fk_import_item_multidisc_blob ON import_item_multidisc_entries(blob_id);

CREATE INDEX fk_import_item_multidisc_upload ON import_item_multidisc_entries(upload_file_id);

CREATE INDEX fk_import_items_job ON import_items(import_job_id);

CREATE INDEX fk_import_job_file_resolutions_replacement
ON import_job_file_resolutions(replacement_import_job_id);

CREATE INDEX fk_import_jobs_platform ON import_jobs(target_platform_instance_id);

CREATE INDEX fk_import_jobs_reconfigured_from
ON import_jobs(reconfigured_from_import_job_id);

CREATE INDEX fk_launch_external_files_blob ON launch_external_files(blob_id);

CREATE INDEX fk_launch_game ON launch_sessions(game_id);

CREATE INDEX fk_launch_validation
ON launch_sessions(rpgmaker_runtime_validation_id,purpose,state);

CREATE INDEX fk_platform_instances_default_core ON platform_instances(default_core_id);

CREATE INDEX fk_upload_files_session ON upload_files(upload_session_id);

CREATE INDEX fk_variant_revision_content ON game_variant_revisions(game_content_revision_id);

CREATE INDEX game_tags_tag ON game_tags(tag_id,game_id);

CREATE INDEX game_variants_game ON game_variants(game_id, core_id);

CREATE INDEX games_library ON games(status, platform_instance_id, search_text, id);

CREATE INDEX import_items_queue ON import_items(state, updated_at_ms, id);

CREATE TRIGGER import_items_review_handoff_kind_immutable
BEFORE UPDATE OF review_handoff_kind ON import_items
WHEN NEW.review_handoff_kind<>OLD.review_handoff_kind
BEGIN SELECT RAISE(ABORT,'immutable import review handoff kind'); END;

CREATE INDEX import_job_file_resolutions_actor
ON import_job_file_resolutions(actor_user_id,created_at_ms);

CREATE INDEX job_events_job ON job_events(job_id,id);

CREATE INDEX job_events_scope ON job_events(scope_type,scope_id,id);

CREATE INDEX jobs_claim ON jobs(state,available_at_ms);

CREATE INDEX jobs_scope ON jobs(scope_type,scope_id);

CREATE UNIQUE INDEX launch_sessions_one_netplay_participant
ON launch_sessions(netplay_session_id,profile_id) WHERE netplay_session_id IS NOT NULL;

CREATE INDEX netplay_events_room ON netplay_events(room_id,id);

CREATE INDEX netplay_events_session ON netplay_events(netplay_session_id,id);

CREATE UNIQUE INDEX netplay_room_members_active_seat
ON netplay_room_members(room_id,player_no) WHERE left_at_ms IS NULL;

CREATE INDEX netplay_room_members_profile ON netplay_room_members(profile_id,left_at_ms,room_id);

CREATE INDEX netplay_rooms_expiry ON netplay_rooms(state,expires_at_ms,id);

CREATE UNIQUE INDEX netplay_rooms_one_active_host
ON netplay_rooms(host_profile_id) WHERE state IN ('DRAFT','WAITING','STARTING','RUNNING');

CREATE INDEX netplay_session_participants_profile
ON netplay_session_participants(profile_id,state,netplay_session_id);

CREATE UNIQUE INDEX netplay_sessions_one_active_room
ON netplay_sessions(room_id) WHERE state NOT IN ('FINISHED','FAILED');

CREATE INDEX netplay_sessions_state ON netplay_sessions(state,updated_at_ms,id);

CREATE INDEX pegasus_collection_tags_tag ON pegasus_collection_tags(tag_id,collection_id);

CREATE INDEX pegasus_collections_mapping ON pegasus_import_collections(import_id,mapping_action,id);

CREATE INDEX pegasus_collections_page ON pegasus_import_collections(import_id,metadata_relative_path,segment_ordinal,id);

CREATE INDEX pegasus_imports_history ON pegasus_imports(created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX pegasus_imports_one_active_execution ON pegasus_imports((1))
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

CREATE INDEX pegasus_imports_state ON pegasus_imports(state,updated_at_ms DESC,id DESC);

CREATE INDEX pegasus_items_collection ON pegasus_import_items(import_id,collection_id,title,id);

CREATE UNIQUE INDEX pegasus_items_library_review ON pegasus_import_items(library_import_item_id)
WHERE library_import_item_id IS NOT NULL;

CREATE INDEX pegasus_items_outcome ON pegasus_import_items(import_id,execution_state,title,id);

CREATE INDEX pegasus_items_page ON pegasus_import_items(import_id,title,id);

CREATE INDEX pegasus_metadata_page ON pegasus_import_metadata_files(import_id,relative_path);

CREATE INDEX emulationstation_collection_tags_tag ON emulationstation_collection_tags(tag_id,collection_id);

CREATE INDEX emulationstation_collections_mapping ON emulationstation_import_collections(import_id,mapping_action,id);

CREATE INDEX emulationstation_collections_page ON emulationstation_import_collections(import_id,gamelist_relative_path,id);

CREATE INDEX emulationstation_gamelists_page ON emulationstation_import_gamelists(import_id,relative_path);

CREATE INDEX emulationstation_imports_history ON emulationstation_imports(created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX emulationstation_imports_one_active_execution ON emulationstation_imports((1))
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

CREATE INDEX emulationstation_imports_state ON emulationstation_imports(state,updated_at_ms DESC,id DESC);

CREATE INDEX emulationstation_items_collection ON emulationstation_import_items(import_id,collection_id,title,id);

CREATE UNIQUE INDEX emulationstation_items_library_review ON emulationstation_import_items(library_import_item_id)
WHERE library_import_item_id IS NOT NULL;

CREATE INDEX emulationstation_items_outcome ON emulationstation_import_items(import_id,execution_state,title,id);

CREATE INDEX emulationstation_items_page ON emulationstation_import_items(import_id,title,id);

CREATE UNIQUE INDEX platform_instances_catalog_template_key_unique
ON platform_instances(catalog_template_key)
WHERE catalog_template_key IS NOT NULL;

CREATE UNIQUE INDEX review_arcade_parent_active
ON review_arcade_parent_attachments(import_item_id)
WHERE state IN ('QUEUED','RUNNING');

CREATE INDEX review_arcade_parent_history
ON review_arcade_parent_attachments(import_item_id,created_at_ms,id);

CREATE INDEX review_bulk_approval_items_state ON review_bulk_approval_items(bulk_approval_id,state,ordinal);

CREATE INDEX review_bulk_approvals_history ON review_bulk_approvals(created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX review_bulk_approvals_one_active ON review_bulk_approvals((1))
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

CREATE INDEX review_draft_tags_tag ON review_draft_tags(tag_id,review_draft_id);

CREATE INDEX review_events_actor ON review_events(actor_user_id,created_at_ms,id);

CREATE INDEX review_events_history ON review_events(event_type,created_at_ms,id);

CREATE TRIGGER review_events_v2_payload_free_insert
BEFORE INSERT ON review_events
WHEN EXISTS(
  SELECT 1
  FROM json_each(json_array(
    NEW.before_json,NEW.after_json,NEW.diff_json,NEW.config_evidence_json,
    NEW.dat_evidence_json,NEW.provider_evidence_json
  )) document
  JOIN json_tree(document.value) node
  WHERE lower(COALESCE(node.key,'')) IN (
    'assetid','candidateassetid','covercandidateassetid','coveruploadedassetid',
    'backgroundcandidateassetid','screenshotcandidateassetids','selectedassets',
    'blobid','archiveblobid','uploadid','uploadfileid','pegasusassetid',
    'url','coverurl','videourl','path','relativepath','sourcepath',
    'sha256','md5','hash','mime','mediatype','widthpx','heightpx',
    'sourcemanifest','sourcemanifestdigest','dependencysnapshot','configsnapshot'
  )
)
BEGIN SELECT RAISE(ABORT,'review event v2 contains payload evidence'); END;

CREATE UNIQUE INDEX review_multidisc_attachment_active
ON review_multidisc_attachments(import_item_id) WHERE state IN ('QUEUED','RUNNING');

CREATE INDEX review_multidisc_attachment_actor
ON review_multidisc_attachments(requested_by_user_id,created_at_ms,id);

CREATE INDEX review_multidisc_attachment_history
ON review_multidisc_attachments(import_item_id,created_at_ms,id);

CREATE INDEX review_preview_files_blob ON review_preview_files(blob_id);

CREATE INDEX review_preview_sessions_actor ON review_preview_sessions(actor_user_id);

CREATE INDEX review_preview_sessions_artifact ON review_preview_sessions(core_artifact_id);

CREATE INDEX review_preview_sessions_item
ON review_preview_sessions(import_item_id,created_at_ms DESC,id DESC);

CREATE INDEX review_preview_sessions_source ON review_preview_sessions(source_snapshot_id);

CREATE INDEX review_preview_sessions_target ON review_preview_sessions(target_platform_instance_id);

CREATE INDEX review_preview_sessions_validation ON review_preview_sessions(validation_id);

CREATE INDEX review_queue ON review_drafts(updated_at_ms, import_item_id);

CREATE UNIQUE INDEX rpgmaker_runtime_validations_passed_binding
ON rpgmaker_runtime_validations(import_item_id,runtime_binding_revision)
WHERE state='PASSED';

CREATE UNIQUE INDEX rpgmaker_validation_gate_terminal
ON rpgmaker_runtime_validation_gate_events(validation_id,gate)
WHERE phase IN ('PASS','FAIL');

CREATE INDEX rpgmaker_validation_gate_launch
ON rpgmaker_runtime_validation_gate_events(launch_id,sequence);

CREATE INDEX review_runtime_screenshots_artifact ON review_runtime_screenshots(core_artifact_id);

CREATE INDEX review_runtime_screenshots_blob ON review_runtime_screenshots(blob_id);

CREATE INDEX review_runtime_screenshots_preview ON review_runtime_screenshots(preview_session_id);

CREATE INDEX review_runtime_screenshots_source ON review_runtime_screenshots(source_snapshot_id);

CREATE INDEX review_runtime_screenshots_validation ON review_runtime_screenshots(validation_id);

CREATE INDEX review_uploaded_assets_item ON review_uploaded_assets(import_item_id, created_at_ms, id);

CREATE INDEX save_states_library ON save_states(profile_id, game_id, created_at_ms DESC, id DESC);

CREATE INDEX save_states_payload ON save_states(payload_blob_id);

CREATE INDEX save_states_source_launch
ON save_states(source_launch_session_id, created_at_ms DESC, id DESC)
WHERE deleted_at_ms IS NULL;

CREATE VIEW save_state_runtime_compatibility AS
SELECT save.id AS save_state_id,
CASE
  WHEN writer.runtime_family IN ('RPGMAKER','ONS','KIRIKIRI','BUTTERSCOTCH','TYRANOSCRIPT') THEN CASE WHEN EXISTS(
    SELECT 1 FROM core_artifacts current
    WHERE current.core_id=writer.core_id AND current.route_key=writer.route_key
      AND current.runtime_family=writer.runtime_family
      AND current.selected_for_new_bindings=1 AND current.available_for_launch=1
      AND json_extract(current.compatibility_json,'$.gameCompatibilityLine')=
          json_extract(writer.compatibility_json,'$.gameCompatibilityLine')
      AND EXISTS(
        SELECT 1 FROM json_each(current.compatibility_json,'$.readableSaveAbis') readable
        WHERE readable.type='text' AND readable.value=save.save_abi
      )
  ) THEN 'AVAILABLE' ELSE 'INCOMPATIBLE_RUNTIME' END
  WHEN writer.available_for_launch=1 THEN 'AVAILABLE'
  ELSE 'CORE_UNAVAILABLE'
END AS status
FROM save_states save
JOIN core_artifacts writer ON writer.id=save.core_artifact_id;

CREATE INDEX server_bios_candidates_page
ON server_bios_import_candidates(server_import_id,requirement_id,rank_ordinal,id);

CREATE UNIQUE INDEX server_bios_candidates_selected
ON server_bios_import_candidates(server_import_id,requirement_id) WHERE state='SELECTED';

CREATE INDEX server_bios_items_page ON server_bios_import_items(server_import_id,core_name_snapshot,logical_name,requirement_id);

CREATE INDEX server_imports_history ON server_imports(kind,created_at_ms DESC,id DESC);

CREATE UNIQUE INDEX server_imports_one_active_kind ON server_imports(kind)
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED');

CREATE INDEX server_imports_state ON server_imports(state,updated_at_ms DESC,id DESC);

CREATE UNIQUE INDEX tags_active_name_key
ON tags(name_key) WHERE status='ACTIVE';

CREATE INDEX tags_active_page ON tags(status,name_key,id);

CREATE INDEX tags_updated_page ON tags(status,updated_at_ms DESC,id DESC);

CREATE UNIQUE INDEX upload_consumptions_whole_session
ON upload_consumptions(upload_session_id) WHERE upload_file_id IS NULL;

CREATE INDEX users_list_created ON users(created_at_ms DESC,id DESC);

CREATE INDEX users_list_last_login ON users(last_login_at_ms DESC,id DESC);

CREATE INDEX users_list_username ON users(username,id);

CREATE TRIGGER account_links_no_delete
BEFORE DELETE ON account_links
BEGIN SELECT RAISE(ABORT, 'account links are retained'); END;

CREATE TRIGGER account_links_terminal_immutable
BEFORE UPDATE ON account_links
WHEN OLD.consumed_at_ms IS NOT NULL OR OLD.revoked_at_ms IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'terminal account link'); END;

CREATE TRIGGER archive_entries_immutable_update
BEFORE UPDATE ON archive_entries
WHEN OLD.materialized_blob_id IS NOT NULL
  OR NEW.materialized_blob_id IS NULL
  OR NEW.archive_blob_id != OLD.archive_blob_id
  OR NEW.ordinal != OLD.ordinal
  OR NEW.original_relative_path != OLD.original_relative_path
  OR NEW.normalized_path != OLD.normalized_path
  OR NEW.ascii_casefold_path != OLD.ascii_casefold_path
  OR NEW.archive_format != OLD.archive_format
  OR NEW.compression_profile != OLD.compression_profile
  OR NEW.uncompressed_size_bytes != OLD.uncompressed_size_bytes
  OR NEW.crc32 != OLD.crc32 OR NEW.md5 != OLD.md5 OR NEW.sha1 != OLD.sha1 OR NEW.sha256 != OLD.sha256
BEGIN
  SELECT RAISE(ABORT, 'archive entry is immutable');
END;

CREATE TRIGGER audit_events_immutable_delete
BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER audit_events_immutable_update
BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER bios_installations_source_insert
BEFORE INSERT ON bios_installations
WHEN (NEW.source_kind='BROWSER_UPLOAD' AND NEW.server_import_candidate_id IS NOT NULL)
  OR (NEW.source_kind='SERVER_DIRECTORY' AND (
    NEW.server_import_candidate_id IS NULL OR NOT EXISTS(
      SELECT 1 FROM server_bios_import_candidates candidate
      WHERE candidate.id=NEW.server_import_candidate_id
      AND candidate.requirement_id=NEW.requirement_id AND candidate.state='SELECTED'
    )
  ))
BEGIN SELECT RAISE(ABORT,'invalid BIOS installation source'); END;

CREATE TRIGGER bios_installations_source_update
BEFORE UPDATE OF source_kind,server_import_candidate_id,requirement_id ON bios_installations
WHEN NEW.source_kind<>OLD.source_kind OR NEW.server_import_candidate_id IS NOT OLD.server_import_candidate_id OR NEW.requirement_id<>OLD.requirement_id
BEGIN SELECT RAISE(ABORT,'immutable BIOS installation source'); END;

CREATE TRIGGER bios_requirements_delivery_insert
BEFORE INSERT ON bios_requirements
WHEN NOT (
  (NEW.delivery_kind='BIOS_BUNDLE' AND NEW.emulator_path IS NULL) OR
  (NEW.delivery_kind='EXTERNAL_FILE' AND
   NEW.emulator_path IS NOT NULL AND
   length(NEW.emulator_path) BETWEEN 1 AND 512 AND
   substr(NEW.emulator_path,1,1)='/' AND
   NEW.emulator_path NOT LIKE '%\%' AND
   NEW.emulator_path NOT LIKE '%?%' AND
   NEW.emulator_path NOT LIKE '%#%' AND
   instr(NEW.emulator_path,char(0))=0 AND
   NEW.emulator_path NOT LIKE '%//%' AND
   NEW.emulator_path NOT LIKE '%/./%' AND
   NEW.emulator_path NOT LIKE '%/../%' AND
   NEW.emulator_path NOT LIKE '%/.' AND
   NEW.emulator_path NOT LIKE '%/..')
)
BEGIN
  SELECT RAISE(ABORT, 'invalid BIOS delivery');
END;

CREATE TRIGGER bios_installations_payload_terminal
BEFORE UPDATE OF blob_id,payload_released_at_ms ON bios_installations
WHEN OLD.blob_id IS NULL AND NEW.blob_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'released BIOS payload is terminal'); END;

CREATE TRIGGER bios_requirements_delivery_update
BEFORE UPDATE OF delivery_kind,emulator_path ON bios_requirements
WHEN NOT (
  (NEW.delivery_kind='BIOS_BUNDLE' AND NEW.emulator_path IS NULL) OR
  (NEW.delivery_kind='EXTERNAL_FILE' AND
   NEW.emulator_path IS NOT NULL AND
   length(NEW.emulator_path) BETWEEN 1 AND 512 AND
   substr(NEW.emulator_path,1,1)='/' AND
   NEW.emulator_path NOT LIKE '%\%' AND
   NEW.emulator_path NOT LIKE '%?%' AND
   NEW.emulator_path NOT LIKE '%#%' AND
   instr(NEW.emulator_path,char(0))=0 AND
   NEW.emulator_path NOT LIKE '%//%' AND
   NEW.emulator_path NOT LIKE '%/./%' AND
   NEW.emulator_path NOT LIKE '%/../%' AND
   NEW.emulator_path NOT LIKE '%/.' AND
   NEW.emulator_path NOT LIKE '%/..')
)
BEGIN
  SELECT RAISE(ABORT, 'invalid BIOS delivery');
END;

CREATE TRIGGER content_hash_evidence_immutable_delete BEFORE DELETE ON content_hash_evidence BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER content_hash_evidence_immutable_update
BEFORE UPDATE ON content_hash_evidence
WHEN NOT (
  OLD.payload_released_at_ms IS NULL AND NEW.payload_released_at_ms IS NOT NULL
  AND NEW.blob_id IS NULL AND NEW.archive_blob_id IS NULL AND NEW.archive_entry_ordinal IS NULL
  AND NEW.id=OLD.id AND NEW.scrape_run_id=OLD.scrape_run_id AND NEW.profile=OLD.profile
  AND NEW.crc32 IS OLD.crc32 AND NEW.md5 IS OLD.md5 AND NEW.sha1 IS OLD.sha1 AND NEW.sha256 IS OLD.sha256
  AND NEW.query_order=OLD.query_order AND NEW.created_at_ms=OLD.created_at_ms
  AND EXISTS(
    SELECT 1 FROM metadata_scrape_runs run
    LEFT JOIN import_items item ON item.id=run.import_item_id
    LEFT JOIN games game ON game.id=run.game_id
    WHERE run.id=OLD.scrape_run_id
      AND (item.payload_state IN ('RELEASING','FAILED') OR game.payload_state IN ('RELEASING','FAILED'))
  )
)
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER content_identity_claims_immutable_delete
BEFORE DELETE ON content_identity_claims
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER content_identity_claims_immutable_update
BEFORE UPDATE ON content_identity_claims
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER dos_entries_immutable_delete BEFORE DELETE ON dos_entries BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER dos_entries_immutable_update BEFORE UPDATE ON dos_entries BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER favorite_folder_games_immutable_update
BEFORE UPDATE ON favorite_folder_games
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER favorite_folders_guarded_update
BEFORE UPDATE ON favorite_folders
WHEN NEW.id<>OLD.id
  OR NEW.profile_id<>OLD.profile_id
  OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.version<>OLD.version+1
  OR NEW.updated_at_ms<OLD.updated_at_ms
  OR (NEW.name=OLD.name AND NEW.name_key=OLD.name_key)
BEGIN
  SELECT RAISE(ABORT,'invalid favorite folder update');
END;

CREATE TRIGGER favorite_games_immutable_update
BEFORE UPDATE ON favorite_games
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER game_assets_immutable_delete BEFORE DELETE ON game_assets
WHEN NOT EXISTS(
  SELECT 1 FROM games
  WHERE id=OLD.game_id AND (
    status='PUBLISHED' AND current_metadata_revision_id<>OLD.metadata_revision_id OR
    status='DELETED' AND payload_state IN ('RELEASING','FAILED')
  )
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_assets_immutable_update BEFORE UPDATE ON game_assets BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_assets_published_insert BEFORE INSERT ON game_assets
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER game_content_files_immutable_delete
BEFORE DELETE ON game_content_files
WHEN NOT EXISTS(
  SELECT 1 FROM game_content_revisions revision JOIN games game ON game.id=revision.game_id
  WHERE revision.id=OLD.game_content_revision_id AND (
    game.status='PUBLISHED' AND game.current_content_revision_id<>revision.id OR
    game.status='DELETED' AND game.payload_state IN ('RELEASING','FAILED')
  )
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_content_files_immutable_update
BEFORE UPDATE ON game_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_content_files_published_insert BEFORE INSERT ON game_content_files
WHEN NOT EXISTS(
  SELECT 1 FROM game_content_revisions revision JOIN games game ON game.id=revision.game_id
  WHERE revision.id=NEW.game_content_revision_id AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER game_content_revisions_immutable_delete BEFORE DELETE ON game_content_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_content_revisions_immutable_update BEFORE UPDATE ON game_content_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_content_revisions_pegasus_source_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1
  FROM pegasus_import_items item
  WHERE item.id=NEW.source_ref_id
  AND item.content_kind=NEW.content_kind
  AND item.execution_state='REVIEW_PENDING'
  AND item.library_import_item_id IS NOT NULL
  AND EXISTS(
    SELECT 1
    FROM review_drafts draft
    JOIN import_item_source_snapshots snapshot
      ON snapshot.id=draft.effective_source_snapshot_id
    WHERE draft.import_item_id=item.library_import_item_id
    AND snapshot.import_item_id=item.library_import_item_id
    AND snapshot.content_kind=NEW.content_kind
    AND snapshot.source_manifest_digest=NEW.source_manifest_digest
  )
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus content source'); END;

CREATE TRIGGER game_content_revisions_emulationstation_source_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='SERVER_EMULATIONSTATION_IMPORT' AND NOT EXISTS(
  SELECT 1
  FROM emulationstation_import_items item
  WHERE item.id=NEW.source_ref_id
  AND item.content_kind=NEW.content_kind
  AND item.execution_state='REVIEW_PENDING'
  AND item.library_import_item_id IS NOT NULL
  AND EXISTS(
    SELECT 1
    FROM review_drafts draft
    JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
    WHERE draft.import_item_id=item.library_import_item_id
    AND snapshot.import_item_id=item.library_import_item_id
    AND snapshot.content_kind=NEW.content_kind
    AND snapshot.source_manifest_digest=NEW.source_manifest_digest
  )
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation content source'); END;

CREATE TRIGGER game_content_revisions_review_snapshot_insert
BEFORE INSERT ON game_content_revisions
WHEN NEW.source_kind='IMPORT_REVIEW' AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=NEW.source_ref_id)
AND NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
  WHERE draft.import_item_id=NEW.source_ref_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
  AND snapshot.content_kind=NEW.content_kind
)
BEGIN SELECT RAISE(ABORT,'review content source snapshot mismatch'); END;

CREATE TRIGGER game_metadata_revisions_immutable_delete BEFORE DELETE ON game_metadata_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_metadata_revisions_immutable_update BEFORE UPDATE ON game_metadata_revisions BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER game_metadata_revisions_pegasus_source_insert
BEFORE INSERT ON game_metadata_revisions
WHEN NEW.source_kind='SERVER_PEGASUS_IMPORT' AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items item WHERE item.id=NEW.source_ref_id
  AND item.execution_state='REVIEW_PENDING'
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus metadata source'); END;

CREATE TRIGGER game_metadata_revisions_emulationstation_source_insert
BEFORE INSERT ON game_metadata_revisions
WHEN NEW.source_kind='SERVER_EMULATIONSTATION_IMPORT' AND NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN import_items public_item ON public_item.id=item.library_import_item_id
  JOIN review_drafts draft ON draft.import_item_id=public_item.id
  WHERE item.id=NEW.source_ref_id AND item.execution_state='REVIEW_PENDING'
    AND public_item.state='REVIEW_PENDING' AND draft.effective_source_snapshot_id IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation metadata source'); END;

CREATE TRIGGER game_tags_immutable_update
BEFORE UPDATE ON game_tags
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER game_tags_validate_insert
BEFORE INSERT ON game_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE')
  OR (SELECT count(*) FROM game_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.game_id=NEW.game_id)>=20
BEGIN
  SELECT RAISE(ABORT,'invalid active game tag');
END;

CREATE TRIGGER game_variant_revisions_immutable_delete BEFORE DELETE ON game_variant_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER game_variant_revisions_immutable_update BEFORE UPDATE ON game_variant_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER game_variants_current_ready_insert
BEFORE INSERT ON game_variants WHEN NEW.current_revision_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_variant_revisions revision
    JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
    WHERE revision.id=NEW.current_revision_id AND revision.game_variant_id=NEW.id AND revision.status='READY'
      AND artifact.core_id=NEW.core_id AND (
        artifact.runtime_family='EMULATORJS' AND revision.emulator_game_id IS NOT NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='RPGMAKER' AND revision.emulator_game_id IS NULL
          AND EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='ONS' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='KIRIKIRI' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='BUTTERSCOTCH' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='TYRANOSCRIPT' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
      )
  ) THEN RAISE(ABORT, 'variant current must be ready and owned') END;
END;

CREATE TRIGGER game_variants_current_ready_update
BEFORE UPDATE OF current_revision_id ON game_variants WHEN NEW.current_revision_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_variant_revisions revision
    JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
    WHERE revision.id=NEW.current_revision_id AND revision.game_variant_id=NEW.id AND revision.status='READY'
      AND artifact.core_id=NEW.core_id AND (
        artifact.runtime_family='EMULATORJS' AND revision.emulator_game_id IS NOT NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='RPGMAKER' AND revision.emulator_game_id IS NULL
          AND EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='ONS' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='KIRIKIRI' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='BUTTERSCOTCH' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
        OR artifact.runtime_family='TYRANOSCRIPT' AND revision.emulator_game_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile WHERE profile.game_variant_revision_id=revision.id)
      )
  ) THEN RAISE(ABORT, 'variant current must be ready and owned') END;
END;

CREATE TRIGGER games_current_metadata_owner_insert
BEFORE INSERT ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_metadata_revisions
    WHERE id=NEW.current_metadata_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_content_revisions
    WHERE id=NEW.current_content_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current content owner mismatch') END;
END;

CREATE TRIGGER games_current_owner_update
BEFORE UPDATE OF current_metadata_revision_id,current_content_revision_id ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_metadata_revisions
    WHERE id=NEW.current_metadata_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS(
    SELECT 1 FROM game_content_revisions
    WHERE id=NEW.current_content_revision_id AND game_id=NEW.id
  ) THEN RAISE(ABORT,'current content owner mismatch') END;
END;

CREATE TRIGGER games_deleted_is_terminal
BEFORE UPDATE OF status ON games
WHEN OLD.status='DELETED' AND NEW.status<>'DELETED'
BEGIN SELECT RAISE(ABORT,'deleted game is terminal'); END;

CREATE TRIGGER import_item_core_validation_artifact_insert
BEFORE INSERT ON import_item_core_validations
WHEN NEW.prepublish_generation<>4 OR NOT EXISTS(
  SELECT 1 FROM core_artifacts artifact
  WHERE artifact.id=NEW.core_artifact_id AND artifact.core_id=NEW.core_id
  AND artifact.version=NEW.core_artifact_version
)
BEGIN SELECT RAISE(ABORT,'invalid validation generation or artifact version'); END;

CREATE TRIGGER import_item_core_validation_snapshot_insert
BEFORE INSERT ON import_item_core_validations
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.source_snapshot_id
  AND snapshot.import_item_id=NEW.import_item_id
  AND snapshot.source_manifest_digest=NEW.source_manifest_digest
)
BEGIN SELECT RAISE(ABORT,'invalid validation source snapshot'); END;

CREATE TRIGGER import_item_core_validations_immutable_delete
BEFORE DELETE ON import_item_core_validations BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_core_validations_immutable_update
BEFORE UPDATE ON import_item_core_validations BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_duplicate_matches_immutable_delete
BEFORE DELETE ON import_item_duplicate_matches
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER import_item_duplicate_matches_immutable_update
BEFORE UPDATE ON import_item_duplicate_matches
BEGIN
  SELECT RAISE(ABORT, 'immutable');
END;

CREATE TRIGGER import_item_multidisc_entries_immutable_delete
BEFORE DELETE ON import_item_multidisc_entries BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_multidisc_entries_immutable_update
BEFORE UPDATE ON import_item_multidisc_entries
WHEN NOT (
  OLD.state='PRESENT' AND NEW.state='PAYLOAD_RELEASED'
  AND NEW.upload_file_id IS NULL AND NEW.blob_id IS NULL AND NEW.payload_released_at_ms IS NOT NULL
  AND EXISTS(
    SELECT 1 FROM import_item_source_snapshots snapshot JOIN import_items item ON item.id=snapshot.import_item_id
    WHERE snapshot.id=OLD.source_snapshot_id AND item.payload_state IN ('RELEASING','FAILED')
  )
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_multidisc_entries_owner_insert
BEFORE INSERT ON import_item_multidisc_entries
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.source_snapshot_id AND snapshot.content_kind='MULTI_DISC_M3U_V1'
)
OR NEW.state='PRESENT' AND NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='DISC'
  AND file.upload_file_id=NEW.upload_file_id AND file.blob_id=NEW.blob_id
  AND file.logical_name=NEW.source_logical_name AND file.sort_order=NEW.ordinal
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc entry owner'); END;

CREATE TRIGGER import_item_source_files_immutable_delete
BEFORE DELETE ON import_item_source_files
WHEN NOT EXISTS(SELECT 1 FROM import_items WHERE id=OLD.import_item_id AND payload_state IN ('RELEASING','FAILED'))
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_source_files_immutable_update
BEFORE UPDATE ON import_item_source_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_source_snapshot_files_immutable_delete
BEFORE DELETE ON import_item_source_snapshot_files
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot JOIN import_items item ON item.id=snapshot.import_item_id
  WHERE snapshot.id=OLD.source_snapshot_id AND item.payload_state IN ('RELEASING','FAILED')
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_source_snapshot_files_immutable_update
BEFORE UPDATE ON import_item_source_snapshot_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_source_snapshots_immutable_delete
BEFORE DELETE ON import_item_source_snapshots BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_source_snapshots_immutable_update
BEFORE UPDATE ON import_item_source_snapshots BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_source_snapshots_revision_insert
BEFORE INSERT ON import_item_source_snapshots
WHEN NEW.revision_no<>(
  SELECT COALESCE(MAX(revision_no),0)+1 FROM import_item_source_snapshots WHERE import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'source snapshot revision must be contiguous'); END;

CREATE TRIGGER import_item_validation_files_immutable_delete
BEFORE DELETE ON import_item_validation_files
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation JOIN import_items item ON item.id=validation.import_item_id
  WHERE validation.id=OLD.import_item_core_validation_id AND item.payload_state IN ('RELEASING','FAILED')
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_validation_files_immutable_update
BEFORE UPDATE ON import_item_validation_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_job_file_resolutions_immutable_delete
BEFORE DELETE ON import_job_file_resolutions BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER import_job_file_resolutions_immutable_update
BEFORE UPDATE ON import_job_file_resolutions BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER instance_state_default_password_no_reenable
BEFORE UPDATE OF test_default_password_active ON instance_state
WHEN OLD.state='COMPLETED' AND OLD.test_default_password_active=0 AND NEW.test_default_password_active=1
BEGIN SELECT RAISE(ABORT, 'test default password cannot be re-enabled'); END;

CREATE TRIGGER instance_state_no_reopen
BEFORE UPDATE OF state ON instance_state
WHEN OLD.state='COMPLETED' AND NEW.state!='COMPLETED'
BEGIN SELECT RAISE(ABORT, 'initialization is terminal'); END;

CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER job_input_snapshots_immutable_delete BEFORE DELETE ON job_input_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER job_input_snapshots_immutable_update BEFORE UPDATE ON job_input_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_content_files_immutable_delete
BEFORE DELETE ON launch_content_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  LEFT JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
  LEFT JOIN games game ON game.id=launch.game_id
  WHERE launch.id=OLD.launch_session_id AND (
    launch.purpose='PRODUCT' AND game.status='PUBLISHED' AND launch.state IN ('FINISHED','EXPIRED','REVOKED') AND (
      game.current_content_revision_id<>revision.game_content_revision_id OR EXISTS(
        SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
        JOIN bios_installations installation
          ON installation.id=json_extract(dependency.value,'$.installationId')
        WHERE installation.payload_released_at_ms IS NOT NULL
      )
    ) OR
    launch.purpose='PRODUCT' AND game.status='DELETED' AND game.payload_state IN ('RELEASING','FAILED')
    OR launch.purpose='RPG_RUNTIME_VALIDATION' AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
      AND EXISTS(SELECT 1 FROM rpgmaker_runtime_validations validation
        WHERE validation.id=launch.rpgmaker_runtime_validation_id
          AND validation.state IN ('PASSED','FAILED','EXPIRED'))
  )
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER launch_content_files_immutable_update
BEFORE UPDATE ON launch_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER launch_content_files_published_insert BEFORE INSERT ON launch_content_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch LEFT JOIN games game ON game.id=launch.game_id
  WHERE launch.id=NEW.launch_session_id AND (
    launch.purpose='PRODUCT' AND game.status='PUBLISHED'
    OR launch.purpose='RPG_RUNTIME_VALIDATION'
      AND launch.effective_source_snapshot_id IS NOT NULL
  )
)
BEGIN SELECT RAISE(ABORT,'launch content owner is invalid'); END;

CREATE TRIGGER launch_external_files_immutable_delete
BEFORE DELETE ON launch_external_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  LEFT JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
  LEFT JOIN games game ON game.id=launch.game_id
  WHERE launch.id=OLD.launch_session_id AND (
    launch.purpose='PRODUCT' AND game.status='PUBLISHED' AND launch.state IN ('FINISHED','EXPIRED','REVOKED') AND (
      game.current_content_revision_id<>revision.game_content_revision_id OR EXISTS(
        SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
        JOIN bios_installations installation
          ON installation.id=json_extract(dependency.value,'$.installationId')
        WHERE installation.payload_released_at_ms IS NOT NULL
      )
    ) OR
    launch.purpose='PRODUCT' AND game.status='DELETED' AND game.payload_state IN ('RELEASING','FAILED')
    OR launch.purpose='RPG_RUNTIME_VALIDATION' AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
      AND EXISTS(SELECT 1 FROM rpgmaker_runtime_validations validation
        WHERE validation.id=launch.rpgmaker_runtime_validation_id
          AND validation.state IN ('PASSED','FAILED','EXPIRED'))
  )
)
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_immutable_update
BEFORE UPDATE ON launch_external_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_published_insert BEFORE INSERT ON launch_external_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch LEFT JOIN games game ON game.id=launch.game_id
  WHERE launch.id=NEW.launch_session_id AND (
    launch.purpose='PRODUCT' AND game.status='PUBLISHED'
    OR launch.purpose='RPG_RUNTIME_VALIDATION'
      AND launch.effective_source_snapshot_id IS NOT NULL
  )
)
BEGIN SELECT RAISE(ABORT,'launch external owner is invalid'); END;

CREATE TRIGGER launch_external_files_kind_insert
BEFORE INSERT ON launch_external_files
WHEN NEW.kind='DISC' AND NOT EXISTS(
  SELECT 1 FROM launch_content_files content
  WHERE content.launch_session_id=NEW.launch_session_id
  AND content.format_version='RETROM_MULTIDISC_M3U_V1'
)
BEGIN SELECT RAISE(ABORT,'disc external file requires multi-disc launch content'); END;

CREATE TRIGGER launch_sessions_netplay_immutable
BEFORE UPDATE OF netplay_session_id,netplay_player_no,save_access ON launch_sessions
BEGIN SELECT RAISE(ABORT,'immutable netplay launch binding'); END;

CREATE TRIGGER launch_sessions_netplay_validate_insert
BEFORE INSERT ON launch_sessions
WHEN NOT (
  NEW.netplay_session_id IS NULL AND NEW.netplay_player_no IS NULL AND NEW.save_access='NORMAL' OR
  NEW.netplay_session_id IS NOT NULL AND NEW.netplay_player_no IS NOT NULL AND NEW.save_access='NETPLAY_DISABLED'
) OR NEW.netplay_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_session_participants participant
  JOIN netplay_sessions session ON session.id=participant.netplay_session_id
  WHERE participant.netplay_session_id=NEW.netplay_session_id AND participant.profile_id=NEW.profile_id
    AND participant.player_no=NEW.netplay_player_no AND session.game_id=NEW.game_id
    AND session.game_variant_revision_id=NEW.game_variant_revision_id
    AND session.core_artifact_id=NEW.core_artifact_id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay launch'); END;

CREATE TRIGGER metadata_provider_cache_owner_insert
BEFORE INSERT ON metadata_provider_cache
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM metadata_provider_responses
    WHERE id = NEW.current_response_id AND provider = NEW.provider AND request_digest = NEW.request_digest
  ) THEN RAISE(ABORT, 'provider cache response mismatch') END;
END;

CREATE TRIGGER netplay_events_immutable_delete
BEFORE DELETE ON netplay_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER netplay_events_immutable_update
BEFORE UPDATE ON netplay_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER netplay_room_members_validate_insert
BEFORE INSERT ON netplay_room_members
WHEN NEW.role='HOST' AND NOT EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.host_profile_id=NEW.profile_id
) OR NEW.role='GUEST' AND EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.host_profile_id=NEW.profile_id
) OR NEW.ready=1 AND NOT EXISTS(
  SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.state='WAITING'
)
BEGIN SELECT RAISE(ABORT,'invalid netplay room member'); END;

CREATE TRIGGER netplay_room_members_validate_update
BEFORE UPDATE ON netplay_room_members
WHEN NEW.room_id!=OLD.room_id OR NEW.profile_id!=OLD.profile_id OR NEW.role!=OLD.role OR
  NEW.role='HOST' AND NEW.player_no!=1 OR NEW.ready=1 AND (
    NEW.left_at_ms IS NOT NULL OR NOT EXISTS(
      SELECT 1 FROM netplay_rooms room WHERE room.id=NEW.room_id AND room.state='WAITING'
    )
  )
BEGIN SELECT RAISE(ABORT,'invalid netplay room member update'); END;

CREATE TRIGGER netplay_rooms_current_session_fk_insert
BEFORE INSERT ON netplay_rooms WHEN NEW.current_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_sessions session WHERE session.id=NEW.current_session_id AND session.room_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay current session'); END;

CREATE TRIGGER netplay_rooms_current_session_fk_update
BEFORE UPDATE OF current_session_id ON netplay_rooms WHEN NEW.current_session_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM netplay_sessions session WHERE session.id=NEW.current_session_id AND session.room_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay current session'); END;

CREATE TRIGGER netplay_rooms_current_session_immutable
BEFORE UPDATE OF current_session_id ON netplay_rooms
WHEN OLD.current_session_id IS NOT NULL AND NEW.current_session_id IS NOT OLD.current_session_id
  AND NEW.state IN ('STARTING','RUNNING')
BEGIN SELECT RAISE(ABORT,'locked netplay room session'); END;

CREATE TRIGGER netplay_rooms_host_immutable
BEFORE UPDATE OF host_profile_id,created_at_ms ON netplay_rooms
BEGIN SELECT RAISE(ABORT,'immutable netplay room identity'); END;

CREATE TRIGGER netplay_rooms_require_host_after_update
AFTER UPDATE ON netplay_rooms WHEN NEW.state IN ('DRAFT','WAITING','STARTING','RUNNING') AND NOT EXISTS(
  SELECT 1 FROM netplay_room_members member
  WHERE member.room_id=NEW.id AND member.profile_id=NEW.host_profile_id AND member.role='HOST'
    AND member.player_no=1 AND member.left_at_ms IS NULL
)
BEGIN SELECT RAISE(ABORT,'active netplay room requires host'); END;

CREATE TRIGGER netplay_rooms_snapshot_immutable
BEFORE UPDATE OF selected_game_id,selected_game_variant_revision_id,netplay_profile_id,profile_digest,max_players
ON netplay_rooms WHEN OLD.state IN ('STARTING','RUNNING')
BEGIN SELECT RAISE(ABORT,'locked netplay room snapshot'); END;

CREATE TRIGGER netplay_session_participants_immutable_identity
BEFORE UPDATE OF netplay_session_id,profile_id,room_member_id,player_no,launch_session_id,credential_sha256,credential_generation
ON netplay_session_participants WHEN OLD.launch_session_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'immutable netplay participant identity'); END;

CREATE TRIGGER netplay_session_participants_validate_insert
BEFORE INSERT ON netplay_session_participants
WHEN NOT EXISTS(
  SELECT 1 FROM netplay_sessions session
  JOIN netplay_room_members member ON member.room_id=session.room_id
  WHERE session.id=NEW.netplay_session_id AND member.id=NEW.room_member_id
    AND member.profile_id=NEW.profile_id AND member.player_no=NEW.player_no AND member.left_at_ms IS NULL
)
BEGIN SELECT RAISE(ABORT,'invalid netplay participant snapshot'); END;

CREATE TRIGGER netplay_sessions_snapshot_immutable
BEFORE UPDATE OF room_id,session_no,game_id,game_variant_revision_id,core_artifact_id,netplay_profile_id,
  profile_json,profile_digest,player_count,occupied_seat_mask,authority_player_no,created_at_ms
ON netplay_sessions
BEGIN SELECT RAISE(ABORT,'immutable netplay session snapshot'); END;

CREATE TRIGGER netplay_sessions_validate_insert
BEFORE INSERT ON netplay_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM netplay_rooms room
  WHERE room.id=NEW.room_id AND room.selected_game_id=NEW.game_id
    AND room.selected_game_variant_revision_id=NEW.game_variant_revision_id
    AND room.netplay_profile_id=NEW.netplay_profile_id AND room.profile_digest=NEW.profile_digest
)
BEGIN SELECT RAISE(ABORT,'invalid netplay session snapshot'); END;

CREATE TRIGGER pegasus_asset_snapshot_update
BEFORE UPDATE ON pegasus_import_item_assets
WHEN NEW.item_id<>OLD.item_id OR NEW.kind<>OLD.kind OR NEW.resolution_method<>OLD.resolution_method OR
  NEW.relative_path<>OLD.relative_path OR NEW.size_bytes IS NOT OLD.size_bytes OR
  NEW.source_facts_digest IS NOT OLD.source_facts_digest OR NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus asset snapshot'); END;

CREATE TRIGGER pegasus_collection_mapping_update
BEFORE UPDATE OF mapping_action,target_platform_instance_id,target_platform_instance_version,target_platform_id,
  target_default_core_id,target_core_artifact_id,target_core_artifact_version,target_dat_version_id,tag_snapshot_json
ON pegasus_import_collections
WHEN NOT EXISTS(SELECT 1 FROM pegasus_imports import WHERE import.id=OLD.import_id AND import.state='AWAITING_MAPPING')
BEGIN
  SELECT RAISE(ABORT,'Pegasus mapping is frozen');
END;

CREATE TRIGGER pegasus_collection_tags_immutable_update
BEFORE UPDATE ON pegasus_collection_tags
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER pegasus_collection_tags_validate_delete
BEFORE DELETE ON pegasus_collection_tags
WHEN EXISTS(SELECT 1 FROM tags WHERE id=OLD.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM pegasus_import_collections collection
    JOIN pegasus_imports import ON import.id=collection.import_id
    WHERE collection.id=OLD.collection_id AND import.state='AWAITING_MAPPING'
  )
BEGIN
  SELECT RAISE(ABORT,'Pegasus collection tag mapping is frozen');
END;

CREATE TRIGGER pegasus_collection_tags_validate_insert
BEFORE INSERT ON pegasus_collection_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM pegasus_import_collections collection
    JOIN pegasus_imports import ON import.id=collection.import_id
    WHERE collection.id=NEW.collection_id AND import.state='AWAITING_MAPPING'
  )
  OR (SELECT count(*) FROM pegasus_collection_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.collection_id=NEW.collection_id)>=20
BEGIN
  SELECT RAISE(ABORT,'invalid active Pegasus collection tag');
END;

CREATE TRIGGER pegasus_file_snapshot_update
BEFORE UPDATE ON pegasus_import_item_files
WHEN NEW.item_id<>OLD.item_id OR NEW.ordinal<>OLD.ordinal OR NEW.declared_kind<>OLD.declared_kind OR
  NEW.relative_path<>OLD.relative_path OR NEW.size_bytes IS NOT OLD.size_bytes OR
  NEW.source_facts_digest IS NOT OLD.source_facts_digest OR NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus file snapshot'); END;

CREATE TRIGGER pegasus_import_job_update
BEFORE UPDATE OF import_job_id ON pegasus_imports
WHEN NEW.import_job_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.import_job_id AND job.kind='SERVER_PEGASUS_IMPORT'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus import job'); END;

CREATE TRIGGER pegasus_import_scan_job_insert
BEFORE INSERT ON pegasus_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.scan_job_id AND job.kind='SERVER_PEGASUS_SCAN'
  AND job.scope_type='PEGASUS_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus scan job'); END;

CREATE TRIGGER pegasus_item_manifest_update
BEFORE UPDATE OF content_kind,source_manifest_json,source_manifest_digest,library_import_job_id,library_import_item_id
ON pegasus_import_items
WHEN OLD.execution_state NOT IN ('COPYING','VALIDATING') OR NEW.execution_state NOT IN ('VALIDATING','REVIEW_PENDING')
BEGIN SELECT RAISE(ABORT,'invalid Pegasus manifest transition'); END;

CREATE TRIGGER pegasus_item_published_update
BEFORE UPDATE OF execution_state,published_game_id ON pegasus_import_items
WHEN NEW.execution_state='PUBLISHED' AND (
  NEW.published_game_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM games game
    JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
    JOIN game_content_revisions content ON content.id=game.current_content_revision_id
    WHERE game.id=NEW.published_game_id AND metadata.source_kind='SERVER_PEGASUS_IMPORT'
    AND metadata.source_ref_id=NEW.id AND content.source_kind='SERVER_PEGASUS_IMPORT'
    AND content.source_ref_id=NEW.id
  )
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus published game'); END;

CREATE TRIGGER pegasus_item_review_discarded_update
BEFORE UPDATE OF execution_state ON pegasus_import_items
WHEN NEW.execution_state='REVIEW_DISCARDED' AND NOT EXISTS(
  SELECT 1 FROM import_items item WHERE item.id=NEW.library_import_item_id AND item.state='DISCARDED'
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus review discard'); END;

CREATE TRIGGER pegasus_item_review_pending_update
BEFORE UPDATE OF execution_state ON pegasus_import_items
WHEN NEW.execution_state='REVIEW_PENDING' AND (
  NEW.library_import_job_id IS NULL OR NEW.library_import_item_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM import_items item
    WHERE item.id=NEW.library_import_item_id AND item.import_job_id=NEW.library_import_job_id
    AND item.state='REVIEW_PENDING'
  )
)
BEGIN SELECT RAISE(ABORT,'invalid Pegasus review handoff'); END;

CREATE TRIGGER pegasus_item_snapshot_update
BEFORE UPDATE ON pegasus_import_items
WHEN NEW.import_id<>OLD.import_id OR NEW.collection_id IS NOT OLD.collection_id OR
  NEW.metadata_relative_path<>OLD.metadata_relative_path OR NEW.game_ordinal<>OLD.game_ordinal OR
  NEW.source_key<>OLD.source_key OR NEW.title<>OLD.title OR NEW.discovery_state<>OLD.discovery_state OR
  NEW.metadata_json<>OLD.metadata_json OR NEW.discovery_code IS NOT OLD.discovery_code OR
  NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus item snapshot'); END;

CREATE TRIGGER pegasus_metadata_files_immutable_update BEFORE UPDATE ON pegasus_import_metadata_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER emulationstation_import_initial_insert
BEFORE INSERT ON emulationstation_imports
WHEN NEW.state<>'SCANNING' OR NEW.phase<>'DISCOVERING_GAMELISTS'
  OR NEW.source_snapshot_digest IS NOT NULL OR NEW.import_job_id IS NOT NULL
  OR NEW.scan_completed_at_ms IS NOT NULL OR NEW.started_at_ms IS NOT NULL OR NEW.completed_at_ms IS NOT NULL
  OR NEW.gamelist_count<>0 OR NEW.invalid_gamelist_count<>0 OR NEW.collection_count<>0
  OR NEW.folder_entry_count<>0 OR NEW.game_count<>0 OR NEW.estimated_source_bytes<>0
  OR NEW.mapped_collection_count<>0 OR NEW.skipped_collection_count<>0
  OR NEW.skipped_mapping_item_count<>0 OR NEW.processable_item_count<>0 OR NEW.blocked_item_count<>0
  OR NEW.review_pending_item_count<>0 OR NEW.published_item_count<>0 OR NEW.review_discarded_item_count<>0
  OR NEW.existing_item_count<>0 OR NEW.failed_item_count<>0 OR NEW.cancelled_item_count<>0
  OR NEW.media_warning_count<>0 OR NEW.discovered_cover_count<>0 OR NEW.discovered_video_count<>0
BEGIN SELECT RAISE(ABORT,'invalid initial EmulationStation import'); END;

CREATE TRIGGER emulationstation_import_identity_update
BEFORE UPDATE OF id,root_id,root_label_snapshot,source_relative_path,root_config_digest,release_year_max,
  scan_job_id,created_by_user_id,created_at_ms,expires_at_ms ON emulationstation_imports
WHEN NEW.id<>OLD.id OR NEW.root_id<>OLD.root_id OR NEW.root_label_snapshot<>OLD.root_label_snapshot
  OR NEW.source_relative_path<>OLD.source_relative_path OR NEW.root_config_digest<>OLD.root_config_digest
  OR NEW.release_year_max<>OLD.release_year_max OR NEW.scan_job_id<>OLD.scan_job_id
  OR NEW.created_by_user_id<>OLD.created_by_user_id OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.expires_at_ms<>OLD.expires_at_ms
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation import identity'); END;

CREATE TRIGGER emulationstation_import_scan_job_insert
BEFORE INSERT ON emulationstation_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.scan_job_id
  AND job.kind='SERVER_EMULATIONSTATION_SCAN'
  AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation scan job'); END;

CREATE TRIGGER emulationstation_import_job_update
BEFORE UPDATE OF import_job_id ON emulationstation_imports
WHEN OLD.import_job_id IS NOT NEW.import_job_id AND (
  OLD.import_job_id IS NOT NULL OR NEW.import_job_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM jobs job WHERE job.id=NEW.import_job_id
    AND job.kind='SERVER_EMULATIONSTATION_IMPORT'
    AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=NEW.id
  )
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation import job'); END;

CREATE TRIGGER emulationstation_import_state_update
BEFORE UPDATE OF state ON emulationstation_imports
WHEN OLD.state<>NEW.state AND NOT (
  OLD.state='SCANNING' AND NEW.state IN ('AWAITING_MAPPING','FAILED') OR
  OLD.state='AWAITING_MAPPING' AND NEW.state IN ('QUEUED','EXPIRED','FAILED') OR
  OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELLED','FAILED') OR
  OLD.state='RUNNING' AND NEW.state IN ('QUEUED','COMPLETED','PARTIAL_FAILURE','CANCEL_REQUESTED','CANCELLED','FAILED') OR
  OLD.state='CANCEL_REQUESTED' AND NEW.state IN ('CANCELLED','FAILED') OR
  OLD.state IN ('PARTIAL_FAILURE','FAILED') AND OLD.retryable=1 AND NEW.state='QUEUED'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation import state transition'); END;

CREATE TRIGGER emulationstation_import_lifecycle_update
BEFORE UPDATE OF state,phase,source_snapshot_digest,import_job_id,scan_completed_at_ms,started_at_ms,completed_at_ms,
  cancel_reason,last_error_code,retryable ON emulationstation_imports
WHEN NOT (
  NEW.state='SCANNING' AND NEW.import_job_id IS NULL AND NEW.source_snapshot_digest IS NULL
    AND NEW.scan_completed_at_ms IS NULL AND NEW.started_at_ms IS NULL AND NEW.completed_at_ms IS NULL
    AND NEW.phase IN ('DISCOVERING_GAMELISTS','PARSING_GAMELISTS','RESOLVING_SOURCES')
  OR NEW.state='AWAITING_MAPPING' AND NEW.import_job_id IS NULL AND NEW.source_snapshot_digest IS NOT NULL
    AND NEW.scan_completed_at_ms IS NOT NULL AND NEW.started_at_ms IS NULL AND NEW.completed_at_ms IS NULL AND NEW.phase IS NULL
  OR NEW.state='QUEUED' AND NEW.import_job_id IS NOT NULL AND NEW.source_snapshot_digest IS NOT NULL
    AND NEW.scan_completed_at_ms IS NOT NULL AND NEW.completed_at_ms IS NULL AND NEW.phase IS NULL
  OR NEW.state='RUNNING' AND NEW.import_job_id IS NOT NULL AND NEW.source_snapshot_digest IS NOT NULL
    AND NEW.scan_completed_at_ms IS NOT NULL AND NEW.started_at_ms IS NOT NULL AND NEW.completed_at_ms IS NULL
    AND NEW.phase IN ('COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS')
  OR NEW.state='CANCEL_REQUESTED' AND NEW.import_job_id IS NOT NULL AND NEW.cancel_reason IS NOT NULL
    AND NEW.completed_at_ms IS NULL AND NEW.phase IN ('COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS')
  OR NEW.state IN ('COMPLETED','PARTIAL_FAILURE','CANCELLED') AND NEW.import_job_id IS NOT NULL
    AND NEW.source_snapshot_digest IS NOT NULL AND NEW.scan_completed_at_ms IS NOT NULL
    AND NEW.completed_at_ms IS NOT NULL AND NEW.phase IS NULL
  OR NEW.state='EXPIRED' AND NEW.import_job_id IS NULL AND NEW.source_snapshot_digest IS NOT NULL
    AND NEW.scan_completed_at_ms IS NOT NULL AND NEW.completed_at_ms IS NOT NULL AND NEW.phase IS NULL
  OR NEW.state='FAILED' AND NEW.completed_at_ms IS NOT NULL AND NEW.phase IS NULL
    AND NEW.last_error_code IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation import lifecycle'); END;

CREATE TRIGGER emulationstation_import_version_update
BEFORE UPDATE ON emulationstation_imports
WHEN NEW.version<OLD.version OR NEW.mapping_version<OLD.mapping_version OR NEW.updated_at_ms<OLD.updated_at_ms
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation import version'); END;

CREATE TRIGGER emulationstation_import_scan_complete_update
BEFORE UPDATE OF state ON emulationstation_imports
WHEN NEW.state='AWAITING_MAPPING' AND (
  NEW.gamelist_count<>(SELECT count(*) FROM emulationstation_import_gamelists source WHERE source.import_id=NEW.id) OR
  NEW.invalid_gamelist_count<>(SELECT count(*) FROM emulationstation_import_gamelists source WHERE source.import_id=NEW.id AND source.parse_state='INVALID') OR
  NEW.collection_count<>(SELECT count(*) FROM emulationstation_import_collections source WHERE source.import_id=NEW.id) OR
  NEW.game_count<>(SELECT count(*) FROM emulationstation_import_items source WHERE source.import_id=NEW.id) OR
  NEW.folder_entry_count<>(SELECT COALESCE(sum(source.folder_count),0) FROM emulationstation_import_gamelists source WHERE source.import_id=NEW.id) OR
  NEW.blocked_item_count<>(SELECT count(*) FROM emulationstation_import_items source WHERE source.import_id=NEW.id AND source.discovery_state<>'READY') OR
  NEW.processable_item_count<>(SELECT count(*) FROM emulationstation_import_items source WHERE source.import_id=NEW.id AND source.discovery_state='READY') OR
  EXISTS(SELECT 1 FROM emulationstation_import_collections collection
    LEFT JOIN emulationstation_import_gamelists source
      ON source.import_id=collection.import_id AND source.relative_path=collection.gamelist_relative_path
    WHERE collection.import_id=NEW.id AND (source.relative_path IS NULL OR source.parse_state<>'VALID'))
)
BEGIN SELECT RAISE(ABORT,'incomplete EmulationStation scan snapshot'); END;

CREATE TRIGGER emulationstation_import_terminal_counts_update
BEFORE UPDATE OF state,skipped_mapping_item_count,review_pending_item_count,published_item_count,
  review_discarded_item_count,existing_item_count,blocked_item_count,failed_item_count,cancelled_item_count
ON emulationstation_imports
WHEN NEW.state IN ('PARTIAL_FAILURE','COMPLETED','CANCELLED','FAILED','EXPIRED') AND (
  NEW.skipped_mapping_item_count+NEW.review_pending_item_count+NEW.published_item_count+
    NEW.review_discarded_item_count+NEW.existing_item_count+NEW.blocked_item_count+
    NEW.failed_item_count+NEW.cancelled_item_count<>NEW.game_count OR
  NEW.skipped_mapping_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state='SKIPPED_MAPPING') OR
  NEW.review_pending_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state='REVIEW_PENDING') OR
  NEW.published_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state='PUBLISHED') OR
  NEW.review_discarded_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state='REVIEW_DISCARDED') OR
  NEW.existing_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state='SKIPPED_EXISTING') OR
  NEW.blocked_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')) OR
  NEW.failed_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')) OR
  NEW.cancelled_item_count<>(SELECT count(*) FROM emulationstation_import_items item WHERE item.import_id=NEW.id AND item.execution_state='CANCELLED')
)
BEGIN SELECT RAISE(ABORT,'invalid terminal EmulationStation counts'); END;

CREATE TRIGGER emulationstation_import_delete
BEFORE DELETE ON emulationstation_imports
WHEN OLD.import_job_id IS NOT NULL OR OLD.state NOT IN ('AWAITING_MAPPING','EXPIRED')
  OR OLD.state='AWAITING_MAPPING' AND EXISTS(
    SELECT 1 FROM emulationstation_import_items item
    WHERE item.import_id=OLD.id AND item.execution_state<>'PENDING'
  )
  OR OLD.state='EXPIRED' AND EXISTS(
    SELECT 1 FROM emulationstation_import_items item
    WHERE item.import_id=OLD.id AND item.execution_state<>'CANCELLED'
  )
BEGIN SELECT RAISE(ABORT,'EmulationStation import is not deletable'); END;

CREATE TRIGGER emulationstation_gamelist_insert
BEFORE INSERT ON emulationstation_import_gamelists
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import WHERE import.id=NEW.import_id AND import.state='SCANNING')
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation gamelist staging insert'); END;

CREATE TRIGGER emulationstation_gamelist_json_insert
BEFORE INSERT ON emulationstation_import_gamelists
WHEN json_array_length(NEW.ignored_fields_json)>64 OR EXISTS(
  SELECT 1 FROM json_each(NEW.ignored_fields_json) entry
  WHERE entry.type<>'text' OR length(entry.value)=0 OR length(CAST(entry.value AS BLOB))>1024
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation ignored fields'); END;

CREATE TRIGGER emulationstation_gamelist_delete
BEFORE DELETE ON emulationstation_import_gamelists
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import
  WHERE import.id=OLD.import_id AND import.state IN ('SCANNING','AWAITING_MAPPING','EXPIRED'))
BEGIN SELECT RAISE(ABORT,'EmulationStation gamelist snapshot is frozen'); END;

CREATE TRIGGER emulationstation_gamelists_immutable_update
BEFORE UPDATE ON emulationstation_import_gamelists
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER emulationstation_collection_insert
BEFORE INSERT ON emulationstation_import_collections
WHEN NOT EXISTS(
  SELECT 1 FROM emulationstation_imports import
  JOIN emulationstation_import_gamelists source ON source.import_id=import.id
  WHERE import.id=NEW.import_id AND import.state='SCANNING'
    AND source.relative_path=NEW.gamelist_relative_path AND source.parse_state='VALID'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation collection owner'); END;

CREATE TRIGGER emulationstation_collection_json_insert
BEFORE INSERT ON emulationstation_import_collections
WHEN json_array_length(NEW.extension_summary_json)>32 OR EXISTS(
  SELECT 1 FROM json_each(NEW.extension_summary_json) entry
  WHERE entry.type<>'object'
    OR (SELECT count(*) FROM json_each(entry.value))<>2
    OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key NOT IN ('extension','count'))
    OR json_type(entry.value,'$.extension')<>'text' OR length(json_extract(entry.value,'$.extension'))=0
    OR json_type(entry.value,'$.count')<>'integer' OR json_extract(entry.value,'$.count')<=0
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation extension summary'); END;

CREATE TRIGGER emulationstation_collection_snapshot_update
BEFORE UPDATE OF id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,issue_count,
  folder_entry_count,hidden_game_count,adult_game_count,extension_summary_json,extension_other_count,created_at_ms
ON emulationstation_import_collections
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation collection snapshot'); END;

CREATE TRIGGER emulationstation_collection_mapping_update
BEFORE UPDATE OF mapping_action,target_platform_instance_id,target_platform_instance_version,target_platform_id,
  target_default_core_id,target_core_artifact_id,target_core_artifact_version,target_dat_version_id,tag_snapshot_json
ON emulationstation_import_collections
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import WHERE import.id=OLD.import_id AND import.state='AWAITING_MAPPING')
BEGIN SELECT RAISE(ABORT,'EmulationStation mapping is frozen'); END;

CREATE TRIGGER emulationstation_collection_mapping_validate_update
BEFORE UPDATE OF mapping_action,target_platform_instance_id,target_platform_instance_version,target_platform_id,
  target_default_core_id,target_core_artifact_id,target_core_artifact_version,target_dat_version_id,tag_snapshot_json
ON emulationstation_import_collections
WHEN NEW.mapping_action='IMPORT' AND (
  NOT EXISTS(
    SELECT 1 FROM platform_instances instance
    JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
    JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
    JOIN platform_cores relation ON relation.platform_id=instance.platform_id
      AND relation.core_id=instance.default_core_id AND relation.enabled=1
    JOIN core_artifacts artifact ON artifact.id=NEW.target_core_artifact_id
      AND artifact.core_id=instance.default_core_id
      AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
    WHERE instance.id=NEW.target_platform_instance_id AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
      AND instance.version=NEW.target_platform_instance_version AND instance.platform_id=NEW.target_platform_id
      AND instance.default_core_id=NEW.target_default_core_id AND artifact.version=NEW.target_core_artifact_version
  ) OR NEW.target_dat_version_id IS NOT NULL AND NOT EXISTS(
    SELECT 1 FROM dat_versions dat WHERE dat.id=NEW.target_dat_version_id
      AND dat.core_id=NEW.target_default_core_id AND dat.core_artifact_id=NEW.target_core_artifact_id
      AND dat.is_active=1 AND dat.parse_status='READY'
  ) OR json_array_length(NEW.tag_snapshot_json)<>(
    SELECT count(*) FROM emulationstation_collection_tags relation
    JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
    WHERE relation.collection_id=NEW.id
  ) OR EXISTS(
    SELECT 1 FROM json_each(NEW.tag_snapshot_json) entry
    LEFT JOIN tags tag ON tag.id=json_extract(entry.value,'$.tagId') AND tag.status='ACTIVE'
    LEFT JOIN emulationstation_collection_tags relation
      ON relation.collection_id=NEW.id AND relation.tag_id=tag.id
    WHERE tag.id IS NULL OR relation.tag_id IS NULL OR tag.name<>json_extract(entry.value,'$.name')
  )
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation mapping target'); END;

CREATE TRIGGER emulationstation_collection_tag_json_update
BEFORE UPDATE OF tag_snapshot_json ON emulationstation_import_collections
WHEN json_array_length(NEW.tag_snapshot_json)>20 OR EXISTS(
  SELECT 1 FROM json_each(NEW.tag_snapshot_json) entry
  WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>2
    OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key NOT IN ('tagId','name'))
    OR json_type(entry.value,'$.tagId')<>'text' OR length(json_extract(entry.value,'$.tagId'))=0
    OR json_type(entry.value,'$.name')<>'text' OR length(json_extract(entry.value,'$.name'))=0
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation tag snapshot'); END;

CREATE TRIGGER emulationstation_collection_delete
BEFORE DELETE ON emulationstation_import_collections
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import
  WHERE import.id=OLD.import_id AND import.state IN ('SCANNING','AWAITING_MAPPING','EXPIRED'))
BEGIN SELECT RAISE(ABORT,'EmulationStation collection snapshot is frozen'); END;

CREATE TRIGGER emulationstation_collection_tags_immutable_update
BEFORE UPDATE ON emulationstation_collection_tags
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER emulationstation_collection_tags_validate_delete
BEFORE DELETE ON emulationstation_collection_tags
WHEN EXISTS(SELECT 1 FROM tags WHERE id=OLD.tag_id AND status='ACTIVE') AND NOT EXISTS(
  SELECT 1 FROM emulationstation_import_collections collection
  JOIN emulationstation_imports import ON import.id=collection.import_id
  WHERE collection.id=OLD.collection_id AND import.state IN ('AWAITING_MAPPING','EXPIRED')
)
BEGIN SELECT RAISE(ABORT,'EmulationStation collection tag mapping is frozen'); END;

CREATE TRIGGER emulationstation_collection_tags_validate_insert
BEFORE INSERT ON emulationstation_collection_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE') OR NOT EXISTS(
    SELECT 1 FROM emulationstation_import_collections collection
    JOIN emulationstation_imports import ON import.id=collection.import_id
    WHERE collection.id=NEW.collection_id AND import.state='AWAITING_MAPPING'
  ) OR (SELECT count(*) FROM emulationstation_collection_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.collection_id=NEW.collection_id)>=20
BEGIN SELECT RAISE(ABORT,'invalid active EmulationStation collection tag'); END;

CREATE TRIGGER emulationstation_item_insert
BEFORE INSERT ON emulationstation_import_items
WHEN NEW.execution_state<>'PENDING' OR NEW.payload_state<>'RETAINED'
  OR NEW.library_import_job_id IS NOT NULL OR NEW.library_import_item_id IS NOT NULL
  OR NEW.published_game_id IS NOT NULL OR NEW.existing_game_id IS NOT NULL OR NEW.existing_content_revision_id IS NOT NULL
  OR NEW.existing_matches_json<>'[]' OR NEW.error_details_json IS NOT NULL OR NEW.completed_at_ms IS NOT NULL
  OR json_array_length(NEW.warnings_json)>64 OR EXISTS(
    SELECT 1 FROM json_each(NEW.warnings_json) entry
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value)) NOT BETWEEN 1 AND 6
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member
        WHERE member.key NOT IN ('code','field','pathKind','omittedCount','originalLength','retainedLength') OR member.type='null')
      OR json_type(entry.value,'$.code')<>'text' OR json_extract(entry.value,'$.code') NOT IN (
        'DUPLICATE_SINGLETON_FIELD','FIELD_IGNORED','FIELD_VALUE_INVALID','FIELD_STRUCTURE_INVALID','FIELD_TRUNCATED',
        'PLAYER_RANGE_NORMALIZED','EMULATIONSTATION_EXECUTION_FIELD_IGNORED','WARNING_LIMIT_REACHED',
        'EMULATIONSTATION_PATH_INVALID','EMULATIONSTATION_MEDIA_MISSING',
        'EMULATIONSTATION_IMAGE_INVALID','EMULATIONSTATION_VIDEO_UNSUPPORTED','EMULATIONSTATION_VIDEO_TOO_LARGE',
        'EMULATIONSTATION_SOURCE_CHANGED','EMULATIONSTATION_MEDIA_READ_FAILED')
      OR json_type(entry.value,'$.field') IS NOT NULL AND json_type(entry.value,'$.field')<>'text'
      OR json_type(entry.value,'$.pathKind') IS NOT NULL AND json_type(entry.value,'$.pathKind')<>'text'
      OR json_type(entry.value,'$.omittedCount') IS NOT NULL AND (
        json_type(entry.value,'$.omittedCount')<>'integer' OR json_extract(entry.value,'$.omittedCount')<1)
      OR json_type(entry.value,'$.originalLength') IS NOT NULL AND (
        json_type(entry.value,'$.originalLength')<>'integer' OR json_extract(entry.value,'$.originalLength')<0)
      OR json_type(entry.value,'$.retainedLength') IS NOT NULL AND (
        json_type(entry.value,'$.retainedLength')<>'integer' OR json_extract(entry.value,'$.retainedLength')<0)
  ) OR NOT EXISTS(
    SELECT 1 FROM emulationstation_imports import
    JOIN emulationstation_import_collections collection ON collection.import_id=import.id
    JOIN emulationstation_import_gamelists source
      ON source.import_id=import.id AND source.relative_path=collection.gamelist_relative_path
    WHERE import.id=NEW.import_id AND import.state='SCANNING' AND collection.id=NEW.collection_id
      AND source.parse_state='VALID' AND NEW.gamelist_relative_path=collection.gamelist_relative_path
  )
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item staging insert'); END;

CREATE TRIGGER emulationstation_item_json_insert
BEFORE INSERT ON emulationstation_import_items
WHEN (SELECT count(*) FROM json_each(NEW.source_flags_json))<>3
  OR EXISTS(SELECT 1 FROM json_each(NEW.source_flags_json) member WHERE member.key NOT IN ('hidden','adult','kidGame'))
  OR json_type(NEW.source_flags_json,'$.hidden') NOT IN ('true','false')
  OR json_type(NEW.source_flags_json,'$.adult') NOT IN ('true','false')
  OR json_type(NEW.source_flags_json,'$.kidGame') NOT IN ('true','false')
  OR (SELECT count(*) FROM json_each(NEW.metadata_json))<>8
  OR EXISTS(SELECT 1 FROM json_each(NEW.metadata_json) member
    WHERE member.key NOT IN ('schemaVersion','title','description','developer','publisher','genre','players','releaseYear'))
  OR json_type(NEW.metadata_json,'$.schemaVersion')<>'integer' OR json_extract(NEW.metadata_json,'$.schemaVersion')<>1
  OR json_type(NEW.metadata_json,'$.title')<>'text' OR json_extract(NEW.metadata_json,'$.title')<>NEW.title
  OR json_type(NEW.metadata_json,'$.description')<>'text' OR json_type(NEW.metadata_json,'$.developer')<>'text'
  OR json_type(NEW.metadata_json,'$.publisher')<>'text' OR json_type(NEW.metadata_json,'$.genre')<>'text'
  OR json_type(NEW.metadata_json,'$.players') NOT IN ('null','integer')
  OR json_type(NEW.metadata_json,'$.players')='integer' AND json_extract(NEW.metadata_json,'$.players') NOT BETWEEN 1 AND 64
  OR json_type(NEW.metadata_json,'$.releaseYear') NOT IN ('null','integer')
  OR json_type(NEW.metadata_json,'$.releaseYear')='integer' AND (
    json_extract(NEW.metadata_json,'$.releaseYear')<1950 OR json_extract(NEW.metadata_json,'$.releaseYear')>(
      SELECT release_year_max FROM emulationstation_imports WHERE id=NEW.import_id
    )
  )
  OR (SELECT count(*) FROM json_each(NEW.source_manifest_json))<>3
  OR EXISTS(SELECT 1 FROM json_each(NEW.source_manifest_json) member WHERE member.key NOT IN ('schemaVersion','contentKind','files'))
  OR json_type(NEW.source_manifest_json,'$.schemaVersion')<>'integer'
  OR json_extract(NEW.source_manifest_json,'$.schemaVersion')<>1
  OR json_type(NEW.source_manifest_json,'$.contentKind')<>'text'
  OR json_extract(NEW.source_manifest_json,'$.contentKind')<>NEW.content_kind
  OR json_type(NEW.source_manifest_json,'$.files')<>'array'
  OR json_array_length(NEW.source_manifest_json,'$.files')>64
  OR EXISTS(
    SELECT 1 FROM json_each(NEW.source_manifest_json,'$.files') entry
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>5
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member
        WHERE member.key NOT IN ('ordinal','declaredKind','relativePath','sizeBytes','sourceFactsDigest'))
      OR json_type(entry.value,'$.ordinal')<>'integer' OR json_extract(entry.value,'$.ordinal')<>CAST(entry.key AS INTEGER)
      OR json_type(entry.value,'$.declaredKind')<>'text'
      OR json_extract(entry.value,'$.declaredKind') NOT IN ('FILE','PLAYLIST','DISC')
      OR json_type(entry.value,'$.relativePath')<>'text'
      OR length(CAST(json_extract(entry.value,'$.relativePath') AS BLOB)) NOT BETWEEN 1 AND 4096
      OR json_type(entry.value,'$.sizeBytes')<>'integer' OR json_extract(entry.value,'$.sizeBytes')<0
      OR json_type(entry.value,'$.sourceFactsDigest')<>'text'
      OR length(json_extract(entry.value,'$.sourceFactsDigest'))<>64
      OR json_extract(entry.value,'$.sourceFactsDigest')<>lower(json_extract(entry.value,'$.sourceFactsDigest'))
  )
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item JSON snapshot'); END;

CREATE TRIGGER emulationstation_item_json_update
BEFORE UPDATE OF warnings_json,existing_matches_json,error_details_json ON emulationstation_import_items
WHEN json_array_length(NEW.warnings_json)>64 OR EXISTS(
    SELECT 1 FROM json_each(NEW.warnings_json) entry
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value)) NOT BETWEEN 1 AND 6
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member
        WHERE member.key NOT IN ('code','field','pathKind','omittedCount','originalLength','retainedLength') OR member.type='null')
      OR json_type(entry.value,'$.code')<>'text' OR json_extract(entry.value,'$.code') NOT IN (
        'DUPLICATE_SINGLETON_FIELD','FIELD_IGNORED','FIELD_VALUE_INVALID','FIELD_STRUCTURE_INVALID','FIELD_TRUNCATED',
        'PLAYER_RANGE_NORMALIZED','EMULATIONSTATION_EXECUTION_FIELD_IGNORED','WARNING_LIMIT_REACHED',
        'EMULATIONSTATION_PATH_INVALID','EMULATIONSTATION_MEDIA_MISSING',
        'EMULATIONSTATION_IMAGE_INVALID','EMULATIONSTATION_VIDEO_UNSUPPORTED','EMULATIONSTATION_VIDEO_TOO_LARGE',
        'EMULATIONSTATION_SOURCE_CHANGED','EMULATIONSTATION_MEDIA_READ_FAILED')
      OR json_type(entry.value,'$.field') IS NOT NULL AND json_type(entry.value,'$.field')<>'text'
      OR json_type(entry.value,'$.pathKind') IS NOT NULL AND json_type(entry.value,'$.pathKind')<>'text'
      OR json_type(entry.value,'$.omittedCount') IS NOT NULL AND (
        json_type(entry.value,'$.omittedCount')<>'integer' OR json_extract(entry.value,'$.omittedCount')<1)
      OR json_type(entry.value,'$.originalLength') IS NOT NULL AND (
        json_type(entry.value,'$.originalLength')<>'integer' OR json_extract(entry.value,'$.originalLength')<0)
      OR json_type(entry.value,'$.retainedLength') IS NOT NULL AND (
        json_type(entry.value,'$.retainedLength')<>'integer' OR json_extract(entry.value,'$.retainedLength')<0)
  ) OR EXISTS(
    SELECT 1 FROM json_each(NEW.existing_matches_json) entry
    LEFT JOIN games game ON game.id=json_extract(entry.value,'$.gameId')
    LEFT JOIN game_content_revisions revision
      ON revision.id=json_extract(entry.value,'$.contentRevisionId') AND revision.game_id=game.id
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>2
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key NOT IN ('gameId','contentRevisionId') OR member.type='null')
      OR json_type(entry.value,'$.gameId')<>'text' OR json_type(entry.value,'$.contentRevisionId')<>'text'
      OR game.id IS NULL OR revision.id IS NULL
  ) OR NEW.error_details_json IS NOT NULL AND (
    (SELECT count(*) FROM json_each(NEW.error_details_json))<>10
    OR EXISTS(SELECT 1 FROM json_each(NEW.error_details_json) member WHERE member.key NOT IN (
      'schemaVersion','stage','operation','causeCode','technicalDetail','relativePath','observedFileCount',
      'allowedFileCount','libraryImportJobId','libraryImportItemId'))
    OR json_type(NEW.error_details_json,'$.schemaVersion')<>'integer'
    OR json_extract(NEW.error_details_json,'$.schemaVersion')<>1
    OR json_type(NEW.error_details_json,'$.stage')<>'text' OR length(json_extract(NEW.error_details_json,'$.stage'))=0
    OR json_type(NEW.error_details_json,'$.operation')<>'text' OR length(json_extract(NEW.error_details_json,'$.operation'))=0
    OR json_type(NEW.error_details_json,'$.causeCode')<>'text' OR json_extract(NEW.error_details_json,'$.causeCode') NOT IN (
      'SOURCE_FILE_LIMIT_EXCEEDED','LIBRARY_IMPORT_INPUT_INVALID','MULTI_DISC_MODE_UNAVAILABLE','DATABASE_BUSY',
      'DATABASE_CONSTRAINT_FAILED','OPERATION_TIMEOUT','OPERATION_CANCELLED','METADATA_JSON_INVALID','INTERNAL_OPERATION_FAILED')
    OR json_type(NEW.error_details_json,'$.technicalDetail')<>'text'
    OR json_type(NEW.error_details_json,'$.relativePath') NOT IN ('null','text')
    OR json_type(NEW.error_details_json,'$.observedFileCount') NOT IN ('null','integer')
    OR json_type(NEW.error_details_json,'$.allowedFileCount') NOT IN ('null','integer')
    OR (json_type(NEW.error_details_json,'$.observedFileCount')='null')<>(json_type(NEW.error_details_json,'$.allowedFileCount')='null')
    OR json_type(NEW.error_details_json,'$.libraryImportJobId') NOT IN ('null','text')
    OR json_type(NEW.error_details_json,'$.libraryImportItemId') NOT IN ('null','text')
  )
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation mutable JSON'); END;

CREATE TRIGGER emulationstation_item_snapshot_update
BEFORE UPDATE OF import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,source_flags_json,
  discovery_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,discovery_code,created_at_ms
ON emulationstation_import_items
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation item snapshot'); END;

CREATE TRIGGER emulationstation_item_state_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN OLD.execution_state<>NEW.execution_state AND NOT (
  OLD.execution_state='PENDING' AND NEW.execution_state IN ('COPYING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='COPYING' AND NEW.execution_state IN ('VALIDATING','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='VALIDATING' AND NEW.execution_state IN ('REVIEW_PENDING','SKIPPED_EXISTING','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='REVIEW_PENDING' AND NEW.execution_state IN ('PUBLISHED','REVIEW_DISCARDED') OR
  OLD.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND OLD.retryable=1 AND NEW.execution_state='PENDING'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item state transition'); END;

CREATE TRIGGER emulationstation_item_execution_update
BEFORE UPDATE OF execution_state,error_code,retryable,library_import_job_id,library_import_item_id,
  published_game_id,existing_game_id,existing_content_revision_id,error_details_json,completed_at_ms
ON emulationstation_import_items
WHEN (NEW.library_import_job_id IS NULL)<>(NEW.library_import_item_id IS NULL)
  OR NEW.execution_state='PUBLISHED' AND (NEW.published_game_id IS NULL OR NEW.existing_game_id IS NOT NULL)
  OR NEW.execution_state<>'PUBLISHED' AND NEW.published_game_id IS NOT NULL
  OR NEW.execution_state='SKIPPED_EXISTING' AND (NEW.existing_game_id IS NULL OR NEW.existing_content_revision_id IS NULL)
  OR NEW.execution_state<>'SKIPPED_EXISTING' AND (NEW.existing_game_id IS NOT NULL OR NEW.existing_content_revision_id IS NOT NULL)
  OR NEW.retryable=1 AND NEW.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
  OR NEW.error_details_json IS NOT NULL AND NEW.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item execution'); END;

CREATE TRIGGER emulationstation_item_version_update
BEFORE UPDATE ON emulationstation_import_items
WHEN NEW.version<OLD.version OR NEW.updated_at_ms<OLD.updated_at_ms
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item version'); END;

CREATE TRIGGER emulationstation_item_library_review_insert
BEFORE INSERT ON emulationstation_import_items
WHEN NEW.library_import_item_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM pegasus_import_items WHERE library_import_item_id=NEW.library_import_item_id
)
BEGIN SELECT RAISE(ABORT,'server source review already owned'); END;

CREATE TRIGGER emulationstation_item_library_review_update
BEFORE UPDATE OF library_import_job_id,library_import_item_id ON emulationstation_import_items
WHEN NEW.library_import_item_id IS NOT NULL AND (
  EXISTS(SELECT 1 FROM pegasus_import_items WHERE library_import_item_id=NEW.library_import_item_id) OR
  NOT EXISTS(SELECT 1 FROM import_items item
    WHERE item.id=NEW.library_import_item_id AND item.import_job_id=NEW.library_import_job_id)
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation review owner'); END;

CREATE TRIGGER pegasus_item_library_review_cross_owner_insert
BEFORE INSERT ON pegasus_import_items
WHEN NEW.library_import_item_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM emulationstation_import_items WHERE library_import_item_id=NEW.library_import_item_id
)
BEGIN SELECT RAISE(ABORT,'server source review already owned'); END;

CREATE TRIGGER pegasus_item_library_review_cross_owner_update
BEFORE UPDATE OF library_import_item_id ON pegasus_import_items
WHEN NEW.library_import_item_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM emulationstation_import_items WHERE library_import_item_id=NEW.library_import_item_id
)
BEGIN SELECT RAISE(ABORT,'server source review already owned'); END;

CREATE TRIGGER emulationstation_item_payload_update
BEFORE UPDATE OF payload_state,payload_release_job_id,payload_released_at_ms,payload_last_error_code
ON emulationstation_import_items
WHEN OLD.payload_state<>NEW.payload_state AND NOT (
  OLD.payload_state='RETAINED' AND NEW.payload_state='RELEASING' OR
  OLD.payload_state='RELEASING' AND NEW.payload_state IN ('RELEASED','FAILED') OR
  OLD.payload_state='FAILED' AND NEW.payload_state IN ('RELEASING','RELEASED')
) OR NEW.payload_release_job_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.payload_release_job_id AND job.kind='PAYLOAD_RELEASE' AND (
    job.scope_type='EMULATIONSTATION_IMPORT_ITEM' AND job.scope_id=NEW.id OR
    job.scope_type='IMPORT_ITEM' AND job.scope_id=NEW.library_import_item_id
  )
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation payload transition'); END;

CREATE TRIGGER emulationstation_item_delete
BEFORE DELETE ON emulationstation_import_items
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import WHERE import.id=OLD.import_id AND (
  import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND OLD.execution_state='PENDING'
  OR import.state='EXPIRED' AND OLD.execution_state='CANCELLED'
))
BEGIN SELECT RAISE(ABORT,'EmulationStation item snapshot is frozen'); END;

CREATE TRIGGER emulationstation_file_insert
BEFORE INSERT ON emulationstation_import_item_files
WHEN NEW.state<>'DISCOVERED' OR NEW.blob_id IS NOT NULL OR NEW.source_archive_blob_id IS NOT NULL
  OR NEW.source_archive_entry_ordinal IS NOT NULL OR NEW.payload_released_at_ms IS NOT NULL OR NOT EXISTS(
    SELECT 1 FROM emulationstation_import_items item
    JOIN emulationstation_imports import ON import.id=item.import_id
    WHERE item.id=NEW.item_id AND import.state='SCANNING'
  )
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation file staging insert'); END;

CREATE TRIGGER emulationstation_file_snapshot_update
BEFORE UPDATE OF item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,created_at_ms
ON emulationstation_import_item_files
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation file snapshot'); END;

CREATE TRIGGER emulationstation_file_state_update
BEFORE UPDATE OF state,blob_id,source_archive_blob_id,source_archive_entry_ordinal,role,logical_name,payload_released_at_ms
ON emulationstation_import_item_files
WHEN OLD.state<>NEW.state AND NOT (
  OLD.state='DISCOVERED' AND NEW.state IN ('COPIED','SOURCE_CHANGED','READ_FAILED','UNSUPPORTED') OR
  OLD.state='COPIED' AND NEW.state='PAYLOAD_RELEASED'
) OR NEW.state='COPIED' AND NEW.blob_id IS NULL
  OR NEW.state IN ('DISCOVERED','SOURCE_CHANGED','READ_FAILED','UNSUPPORTED','PAYLOAD_RELEASED') AND NEW.blob_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation file transition'); END;

CREATE TRIGGER emulationstation_file_delete
BEFORE DELETE ON emulationstation_import_item_files
WHEN NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN emulationstation_imports import ON import.id=item.import_id
  WHERE item.id=OLD.item_id AND (
    import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND item.execution_state='PENDING'
    OR import.state='EXPIRED' AND item.execution_state='CANCELLED'
  )
)
BEGIN SELECT RAISE(ABORT,'EmulationStation file snapshot is frozen'); END;

CREATE TRIGGER emulationstation_asset_insert
BEFORE INSERT ON emulationstation_import_item_assets
WHEN NEW.blob_id IS NOT NULL OR NEW.payload_released_at_ms IS NOT NULL OR NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN emulationstation_imports import ON import.id=item.import_id
  WHERE item.id=NEW.item_id AND import.state='SCANNING'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation asset staging insert'); END;

CREATE TRIGGER emulationstation_asset_snapshot_update
BEFORE UPDATE OF item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,media_type,width_px,height_px,created_at_ms
ON emulationstation_import_item_assets
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation asset snapshot'); END;

CREATE TRIGGER emulationstation_asset_state_update
BEFORE UPDATE OF state,blob_id,payload_released_at_ms,warning_code ON emulationstation_import_item_assets
WHEN OLD.state<>NEW.state AND NOT (
  OLD.state='DISCOVERED' AND NEW.state IN ('COPIED','SOURCE_CHANGED','READ_FAILED') OR
  OLD.state='COPIED' AND NEW.state='PAYLOAD_RELEASED'
) OR NEW.state='COPIED' AND NEW.blob_id IS NULL
  OR NEW.state IN ('DISCOVERED','MISSING','INVALID','TOO_LARGE','SOURCE_CHANGED','READ_FAILED','PAYLOAD_RELEASED') AND NEW.blob_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation asset transition'); END;

CREATE TRIGGER emulationstation_asset_delete
BEFORE DELETE ON emulationstation_import_item_assets
WHEN NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items item
  JOIN emulationstation_imports import ON import.id=item.import_id
  WHERE item.id=OLD.item_id AND (
    import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND item.execution_state='PENDING'
    OR import.state='EXPIRED' AND item.execution_state='CANCELLED'
  )
)
BEGIN SELECT RAISE(ABORT,'EmulationStation asset snapshot is frozen'); END;

CREATE TRIGGER emulationstation_item_published_update
BEFORE UPDATE OF execution_state,published_game_id ON emulationstation_import_items
WHEN NEW.execution_state='PUBLISHED' AND (
  NEW.published_game_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM games game
    JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
    JOIN game_content_revisions content ON content.id=game.current_content_revision_id
    WHERE game.id=NEW.published_game_id AND metadata.source_kind='SERVER_EMULATIONSTATION_IMPORT'
    AND metadata.source_ref_id=NEW.id AND content.source_kind='SERVER_EMULATIONSTATION_IMPORT'
    AND content.source_ref_id=NEW.id
  )
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation published game'); END;

CREATE TRIGGER emulationstation_item_review_discarded_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN NEW.execution_state='REVIEW_DISCARDED' AND NOT EXISTS(
  SELECT 1 FROM import_items item
  JOIN review_events event ON event.import_item_id=item.id AND event.event_type='DISCARDED'
  WHERE item.id=NEW.library_import_item_id AND item.state='DISCARDED'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation review discard'); END;

CREATE TRIGGER emulationstation_item_review_pending_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN NEW.execution_state='REVIEW_PENDING' AND (
  NEW.library_import_job_id IS NULL OR NEW.library_import_item_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM import_items item
    WHERE item.id=NEW.library_import_item_id AND item.import_job_id=NEW.library_import_job_id
    AND item.state='REVIEW_PENDING'
  )
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation review handoff'); END;

CREATE TRIGGER platform_cores_in_use_disable
BEFORE UPDATE OF enabled ON platform_cores WHEN NEW.enabled = 0
BEGIN
  SELECT CASE WHEN EXISTS (
    SELECT 1 FROM platform_instances WHERE platform_id = OLD.platform_id AND default_core_id = OLD.core_id AND deleted_at_ms IS NULL
  ) THEN RAISE(ABORT, 'platform core is in use') END;
END;

CREATE TRIGGER platform_instances_enabled_default_insert
BEFORE INSERT ON platform_instances
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM platform_cores
    WHERE platform_id = NEW.platform_id AND core_id = NEW.default_core_id AND enabled = 1
  ) THEN RAISE(ABORT, 'platform default core is not enabled') END;
END;

CREATE TRIGGER platform_instances_enabled_default_update
BEFORE UPDATE OF default_core_id ON platform_instances
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM platform_cores
    WHERE platform_id = NEW.platform_id AND core_id = NEW.default_core_id AND enabled = 1
  ) THEN RAISE(ABORT, 'platform default core is not enabled') END;
END;

CREATE TRIGGER play_session_events_immutable_delete BEFORE DELETE ON play_session_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER play_session_events_immutable_update BEFORE UPDATE ON play_session_events BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER profiles_require_user_after_initialization
BEFORE INSERT ON profiles
WHEN (SELECT state FROM instance_state WHERE id=1)='COMPLETED'
BEGIN
  SELECT CASE WHEN NEW.id='local' THEN RAISE(ABORT, 'reserved profile') END;
END;

CREATE TRIGGER provider_responses_immutable_delete BEFORE DELETE ON metadata_provider_responses BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER provider_responses_immutable_update
BEFORE UPDATE ON metadata_provider_responses
WHEN NOT (
  OLD.raw_payload_state='RETAINED' AND NEW.raw_payload_state='RELEASED'
  AND NEW.raw_response_blob_id IS NULL AND NEW.raw_payload_released_at_ms IS NOT NULL
  AND NEW.id=OLD.id AND NEW.provider=OLD.provider AND NEW.request_digest=OLD.request_digest
  AND NEW.http_status IS OLD.http_status AND NEW.outcome=OLD.outcome
  AND NEW.fetched_at_ms=OLD.fetched_at_ms AND NEW.expires_at_ms=OLD.expires_at_ms
)
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER review_arcade_parent_owner_insert
BEFORE INSERT ON review_arcade_parent_attachments
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.base_source_snapshot_id
  WHERE draft.id=NEW.review_draft_id
  AND draft.import_item_id=NEW.import_item_id
  AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'invalid attachment owner'); END;

CREATE TRIGGER review_arcade_parent_transition_update
BEFORE UPDATE OF state ON review_arcade_parent_attachments
WHEN NOT (
  OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELLED','FAILED_RETRYABLE') OR
  OLD.state='RUNNING' AND NEW.state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED') OR
  OLD.state='FAILED_RETRYABLE' AND NEW.state IN ('QUEUED','RUNNING','CANCELLED')
)
BEGIN SELECT RAISE(ABORT, 'invalid attachment state transition'); END;

CREATE TRIGGER review_bulk_approval_items_frozen_update
BEFORE UPDATE OF bulk_approval_id,import_item_id,ordinal,expected_review_version,expected_validation_id,
expected_source_snapshot_id,title_snapshot,target_platform_instance_id,target_platform_name_snapshot,
created_at_ms ON review_bulk_approval_items
BEGIN SELECT RAISE(ABORT,'immutable review bulk approval item input'); END;

CREATE TRIGGER review_bulk_approval_items_owner_insert
BEFORE INSERT ON review_bulk_approval_items
WHEN NOT EXISTS(
  SELECT 1 FROM import_items item
  JOIN review_drafts draft ON draft.import_item_id=item.id
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.expected_source_snapshot_id
  JOIN import_item_core_validations validation ON validation.id=NEW.expected_validation_id
  WHERE item.id=NEW.import_item_id AND item.state='REVIEW_PENDING'
  AND draft.version=NEW.expected_review_version
  AND draft.effective_source_snapshot_id=NEW.expected_source_snapshot_id
  AND draft.target_platform_instance_id=NEW.target_platform_instance_id
  AND snapshot.import_item_id=NEW.import_item_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.expected_source_snapshot_id
  AND validation.target_platform_instance_id=NEW.target_platform_instance_id
)
BEGIN SELECT RAISE(ABORT,'invalid review bulk approval item input'); END;

CREATE TRIGGER review_bulk_approval_items_published_update
BEFORE UPDATE OF state,game_id,review_event_id ON review_bulk_approval_items
WHEN NEW.state='PUBLISHED' AND NOT EXISTS(
  SELECT 1 FROM review_events event
  JOIN games game ON game.id=NEW.game_id
  WHERE event.id=NEW.review_event_id AND event.import_item_id=NEW.import_item_id
  AND event.event_type='APPROVED' AND json_extract(event.after_json,'$.gameId')=NEW.game_id
)
BEGIN SELECT RAISE(ABORT,'invalid review bulk approval published result'); END;

CREATE TRIGGER review_bulk_approval_job_insert
BEFORE INSERT ON review_bulk_approvals
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.job_id AND job.kind='REVIEW_BULK_APPROVE'
  AND job.scope_type='REVIEW_BULK_APPROVAL' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid review bulk approval job'); END;

CREATE TRIGGER review_bulk_approvals_frozen_update
BEFORE UPDATE OF job_id,scope_json,scope_digest,candidate_manifest_digest,matched_count,candidate_count,
screenshot_only_count,duplicate_count,attachment_active_count,source_flagged_count,not_ready_or_stale_count,
created_by_user_id,created_at_ms ON review_bulk_approvals
BEGIN SELECT RAISE(ABORT,'immutable review bulk approval input'); END;

CREATE TRIGGER review_draft_tags_immutable_update
BEFORE UPDATE ON review_draft_tags
BEGIN
  SELECT RAISE(ABORT,'immutable');
END;

CREATE TRIGGER review_draft_tags_validate_delete
BEFORE DELETE ON review_draft_tags
WHEN EXISTS(SELECT 1 FROM tags WHERE id=OLD.tag_id AND status='ACTIVE')
  AND NOT EXISTS(
    SELECT 1 FROM review_drafts draft
    JOIN import_items item ON item.id=draft.import_item_id
    WHERE draft.id=OLD.review_draft_id AND item.state='REVIEW_PENDING'
  )
BEGIN
  SELECT RAISE(ABORT,'review tag mapping is frozen');
END;

CREATE TRIGGER review_draft_tags_validate_insert
BEFORE INSERT ON review_draft_tags
WHEN NOT EXISTS(SELECT 1 FROM tags WHERE id=NEW.tag_id AND status='ACTIVE')
  OR NOT EXISTS(
    SELECT 1 FROM review_drafts draft
    JOIN import_items item ON item.id=draft.import_item_id
    WHERE draft.id=NEW.review_draft_id AND item.state='REVIEW_PENDING'
  )
  OR (SELECT count(*) FROM review_draft_tags relation
      JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
      WHERE relation.review_draft_id=NEW.review_draft_id)>=20
BEGIN
  SELECT RAISE(ABORT,'invalid active review tag');
END;

CREATE TRIGGER review_drafts_final_source_snapshot_update
BEFORE UPDATE OF effective_source_snapshot_id ON review_drafts
WHEN NEW.effective_source_snapshot_id<>OLD.effective_source_snapshot_id
AND EXISTS(SELECT 1 FROM import_items item WHERE item.id=OLD.import_item_id AND item.state<>'REVIEW_PENDING')
BEGIN SELECT RAISE(ABORT, 'finalized review source snapshot'); END;

CREATE TRIGGER review_drafts_runtime_binding_revision_update
BEFORE UPDATE OF runtime_binding_revision ON review_drafts
WHEN NEW.runtime_binding_revision<>OLD.runtime_binding_revision AND (
  NEW.runtime_binding_revision<>OLD.runtime_binding_revision+1
  OR NOT EXISTS(SELECT 1 FROM import_items item
    WHERE item.id=OLD.import_item_id AND item.state='REVIEW_PENDING')
)
BEGIN SELECT RAISE(ABORT,'runtime binding revision must increment by one'); END;

CREATE TRIGGER review_drafts_source_snapshot_insert
BEFORE INSERT ON review_drafts
WHEN NEW.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.effective_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'invalid review source snapshot'); END;

CREATE TRIGGER review_drafts_source_snapshot_update
BEFORE UPDATE OF import_item_id,effective_source_snapshot_id ON review_drafts
WHEN NEW.effective_source_snapshot_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  WHERE snapshot.id=NEW.effective_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
)
BEGIN SELECT RAISE(ABORT,'invalid review source snapshot'); END;

CREATE TRIGGER review_drafts_uploaded_cover_insert BEFORE INSERT ON review_drafts
WHEN NEW.cover_uploaded_asset_id IS NOT NULL
AND (
  NEW.cover_candidate_asset_id IS NOT NULL
  OR NOT EXISTS (
    SELECT 1 FROM review_uploaded_assets a
    WHERE a.id=NEW.cover_uploaded_asset_id
    AND a.import_item_id=NEW.import_item_id
    AND a.kind='COVER'
  )
)
BEGIN SELECT RAISE(ABORT, 'invalid review uploaded cover'); END;

CREATE TRIGGER review_drafts_uploaded_cover_update BEFORE UPDATE OF import_item_id,cover_candidate_asset_id,cover_uploaded_asset_id ON review_drafts
WHEN NEW.cover_uploaded_asset_id IS NOT NULL
AND (
  NEW.cover_candidate_asset_id IS NOT NULL
  OR NOT EXISTS (
    SELECT 1 FROM review_uploaded_assets a
    WHERE a.id=NEW.cover_uploaded_asset_id
    AND a.import_item_id=NEW.import_item_id
    AND a.kind='COVER'
  )
)
BEGIN SELECT RAISE(ABORT, 'invalid review uploaded cover'); END;

CREATE TRIGGER review_drafts_validation_snapshot_insert
BEFORE INSERT ON review_drafts
WHEN NEW.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.selected_validation_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.effective_source_snapshot_id
  AND validation.status='READY' AND validation.prepublish_generation=4
)
BEGIN SELECT RAISE(ABORT,'invalid selected validation snapshot'); END;

CREATE TRIGGER review_drafts_validation_snapshot_update
BEFORE UPDATE OF import_item_id,effective_source_snapshot_id,selected_validation_id ON review_drafts
WHEN NEW.selected_validation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.selected_validation_id
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.effective_source_snapshot_id
  AND validation.status='READY' AND validation.prepublish_generation=4
)
BEGIN SELECT RAISE(ABORT,'invalid selected validation snapshot'); END;

CREATE TRIGGER review_events_immutable_delete
BEFORE DELETE ON review_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_events_immutable_update
BEFORE UPDATE ON review_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_multidisc_attachment_delete
BEFORE DELETE ON review_multidisc_attachments BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_multidisc_attachment_identity_update
BEFORE UPDATE OF import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,
upload_session_id,expected_set_digest,job_id,created_at_ms ON review_multidisc_attachments
BEGIN SELECT RAISE(ABORT,'multi-disc attachment identity is immutable'); END;

CREATE TRIGGER review_multidisc_attachment_owner_insert
BEFORE INSERT ON review_multidisc_attachments
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.base_source_snapshot_id
  JOIN jobs job ON job.id=NEW.job_id
  WHERE draft.id=NEW.review_draft_id AND draft.import_item_id=NEW.import_item_id
  AND draft.effective_source_snapshot_id=NEW.base_source_snapshot_id
  AND snapshot.import_item_id=NEW.import_item_id AND snapshot.content_kind='MULTI_DISC_M3U_V1'
  AND job.scope_type='IMPORT_ITEM' AND job.scope_id=NEW.import_item_id
  AND job.kind='REVIEW_MULTI_DISC_VALIDATE'
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment owner'); END;

CREATE TRIGGER review_multidisc_attachment_result_update
BEFORE UPDATE OF state,result_source_snapshot_id,result_validation_id ON review_multidisc_attachments
WHEN NEW.state='ACCEPTED' AND NOT EXISTS(
  SELECT 1 FROM import_item_source_snapshots snapshot
  JOIN import_item_core_validations validation ON validation.id=NEW.result_validation_id
  WHERE snapshot.id=NEW.result_source_snapshot_id AND snapshot.import_item_id=NEW.import_item_id
  AND snapshot.content_kind='MULTI_DISC_M3U_V1' AND snapshot.created_by='MULTI_DISC_ATTACHMENT'
  AND snapshot.revision_no=(
    SELECT revision_no+1 FROM import_item_source_snapshots WHERE id=NEW.base_source_snapshot_id
  )
  AND validation.import_item_id=NEW.import_item_id
  AND validation.source_snapshot_id=NEW.result_source_snapshot_id
  AND validation.prepublish_generation=4
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment result'); END;

CREATE TRIGGER review_multidisc_attachment_terminal_update
BEFORE UPDATE ON review_multidisc_attachments
WHEN OLD.state IN ('ACCEPTED','REJECTED','CANCELLED')
BEGIN SELECT RAISE(ABORT,'terminal multi-disc attachment is immutable'); END;

CREATE TRIGGER review_multidisc_attachment_transition_update
BEFORE UPDATE OF state ON review_multidisc_attachments
WHEN NOT (
  OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELLED') OR
  OLD.state='RUNNING' AND NEW.state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED') OR
  OLD.state='FAILED_RETRYABLE' AND NEW.state IN ('RUNNING','CANCELLED')
)
BEGIN SELECT RAISE(ABORT,'invalid multi-disc attachment state transition'); END;

CREATE TRIGGER review_preview_files_immutable_delete
BEFORE DELETE ON review_preview_files
WHEN NOT EXISTS(
  SELECT 1 FROM review_preview_sessions preview JOIN import_items item ON item.id=preview.import_item_id
  WHERE preview.id=OLD.preview_session_id AND item.payload_state IN ('RELEASING','FAILED')
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_preview_files_immutable_update
BEFORE UPDATE ON review_preview_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER review_preview_sessions_validate_insert
BEFORE INSERT ON review_preview_sessions
WHEN NOT EXISTS (
  SELECT 1 FROM import_items item
  JOIN review_drafts draft ON draft.import_item_id=item.id
  JOIN import_item_source_snapshots snapshot ON snapshot.id=NEW.source_snapshot_id
    AND snapshot.import_item_id=item.id
  JOIN import_item_core_validations validation ON validation.id=NEW.validation_id
    AND validation.import_item_id=item.id
    AND validation.source_snapshot_id=NEW.source_snapshot_id
    AND validation.target_platform_instance_id=NEW.target_platform_instance_id
    AND validation.core_artifact_id=NEW.core_artifact_id
    AND validation.prepublish_generation=4
  JOIN users actor ON actor.id=NEW.actor_user_id AND actor.role='ADMIN' AND actor.status='ENABLED'
  WHERE item.id=NEW.import_item_id AND item.state='REVIEW_PENDING'
    AND draft.effective_source_snapshot_id=NEW.source_snapshot_id
    AND draft.target_platform_instance_id=NEW.target_platform_instance_id
    AND validation.id=(
      SELECT candidate.id FROM import_item_core_validations candidate
      WHERE candidate.import_item_id=NEW.import_item_id
        AND candidate.source_snapshot_id=NEW.source_snapshot_id
        AND candidate.target_platform_instance_id=NEW.target_platform_instance_id
        AND candidate.core_artifact_id=NEW.core_artifact_id
      ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
    )
) OR NOT (
  NEW.content_kind='SINGLE_FILE' AND EXISTS (
    SELECT 1 FROM import_item_source_snapshot_files file
    WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='CONTENT'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='DOS_BUNDLE' AND EXISTS (
    SELECT 1 FROM import_item_validation_files file
    WHERE file.import_item_core_validation_id=NEW.validation_id AND file.role='DOS_LAUNCH_BUNDLE'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='MULTI_DISC_M3U_V1' AND EXISTS (
    SELECT 1 FROM import_item_validation_files file
    WHERE file.import_item_core_validation_id=NEW.validation_id AND file.role='MULTI_DISC_PLAYLIST'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='ONS_PROJECT_V1' AND NEW.content_format='ONS_PROJECT_V1' AND EXISTS (
    SELECT 1 FROM import_item_source_snapshot_files file
    WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='PROJECT_FILE'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='KIRIKIRI_PROJECT_V1' AND NEW.content_format='KIRIKIRI_PROJECT_V1' AND EXISTS (
    SELECT 1 FROM import_item_source_snapshot_files file
    WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='PROJECT_FILE'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='BUTTERSCOTCH_PROJECT_V1' AND NEW.content_format='BUTTERSCOTCH_PROJECT_V1' AND EXISTS (
    SELECT 1 FROM import_item_source_snapshot_files file
    WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='PROJECT_FILE'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  ) OR NEW.content_kind='TYRANOSCRIPT_PROJECT_V1' AND NEW.content_format='TYRANOSCRIPT_PROJECT_V1' AND EXISTS (
    SELECT 1 FROM import_item_source_snapshot_files file
    WHERE file.source_snapshot_id=NEW.source_snapshot_id AND file.role='PROJECT_FILE'
      AND file.blob_id=NEW.content_blob_id AND file.logical_name=NEW.content_logical_name
  )
)
BEGIN SELECT RAISE(ABORT,'invalid review preview snapshot'); END;

CREATE TRIGGER review_preview_files_validate_insert
BEFORE INSERT ON review_preview_files
WHEN NEW.role='PROJECT_FILE' AND NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN import_item_source_snapshot_files source
    ON source.source_snapshot_id=preview.source_snapshot_id
    AND source.role='PROJECT_FILE'
    AND source.logical_name=NEW.logical_name
    AND source.blob_id=NEW.blob_id
  WHERE preview.id=NEW.preview_session_id
    AND preview.content_kind IN (
      'ONS_PROJECT_V1','KIRIKIRI_PROJECT_V1','BUTTERSCOTCH_PROJECT_V1','TYRANOSCRIPT_PROJECT_V1'
    )
)
BEGIN SELECT RAISE(ABORT,'invalid review preview project file'); END;

CREATE TRIGGER review_runtime_screenshots_validate_insert
BEFORE INSERT ON review_runtime_screenshots
WHEN NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN review_drafts draft ON draft.import_item_id=preview.import_item_id
    AND draft.effective_source_snapshot_id=preview.source_snapshot_id
    AND draft.target_platform_instance_id=preview.target_platform_instance_id
  JOIN import_item_core_validations validation ON validation.id=preview.validation_id
    AND validation.import_item_id=preview.import_item_id
    AND validation.source_snapshot_id=preview.source_snapshot_id
    AND validation.target_platform_instance_id=preview.target_platform_instance_id
    AND validation.core_artifact_id=preview.core_artifact_id
    AND validation.prepublish_generation=4
  WHERE preview.id=NEW.preview_session_id AND preview.capture_allowed=1
    AND preview.import_item_id=NEW.import_item_id
    AND preview.source_snapshot_id=NEW.source_snapshot_id
    AND preview.validation_id=NEW.validation_id
    AND preview.core_artifact_id=NEW.core_artifact_id
    AND validation.id=(
      SELECT candidate.id FROM import_item_core_validations candidate
      WHERE candidate.import_item_id=preview.import_item_id
        AND candidate.source_snapshot_id=preview.source_snapshot_id
        AND candidate.target_platform_instance_id=preview.target_platform_instance_id
        AND candidate.core_artifact_id=preview.core_artifact_id
      ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
    )
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime screenshot'); END;

CREATE TRIGGER review_runtime_screenshots_validate_update
BEFORE UPDATE ON review_runtime_screenshots
WHEN NOT EXISTS (
  SELECT 1 FROM review_preview_sessions preview
  JOIN review_drafts draft ON draft.import_item_id=preview.import_item_id
    AND draft.effective_source_snapshot_id=preview.source_snapshot_id
    AND draft.target_platform_instance_id=preview.target_platform_instance_id
  JOIN import_item_core_validations validation ON validation.id=preview.validation_id
    AND validation.import_item_id=preview.import_item_id
    AND validation.source_snapshot_id=preview.source_snapshot_id
    AND validation.target_platform_instance_id=preview.target_platform_instance_id
    AND validation.core_artifact_id=preview.core_artifact_id
    AND validation.prepublish_generation=4
  WHERE preview.id=NEW.preview_session_id AND preview.capture_allowed=1
    AND preview.import_item_id=NEW.import_item_id
    AND preview.source_snapshot_id=NEW.source_snapshot_id
    AND preview.validation_id=NEW.validation_id
    AND preview.core_artifact_id=NEW.core_artifact_id
    AND validation.id=(
      SELECT candidate.id FROM import_item_core_validations candidate
      WHERE candidate.import_item_id=preview.import_item_id
        AND candidate.source_snapshot_id=preview.source_snapshot_id
        AND candidate.target_platform_instance_id=preview.target_platform_instance_id
        AND candidate.core_artifact_id=preview.core_artifact_id
      ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
    )
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime screenshot'); END;

CREATE TRIGGER review_uploaded_assets_immutable_delete BEFORE DELETE ON review_uploaded_assets
WHEN NOT EXISTS(SELECT 1 FROM import_items WHERE id=OLD.import_item_id AND payload_state IN ('RELEASING','FAILED'))
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER review_uploaded_assets_immutable_update BEFORE UPDATE ON review_uploaded_assets
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER save_states_disc_insert
BEFORE INSERT ON save_states
WHEN (
  EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND (
    NEW.disc_index IS NULL OR NEW.disc_index >= (
      SELECT count(*) FROM launch_external_files external
      WHERE external.launch_session_id=NEW.source_launch_session_id AND external.kind='DISC'
    )
  )
  OR NOT EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND NEW.disc_index IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'save state disc index mismatch'); END;

CREATE TRIGGER save_states_disc_update
BEFORE UPDATE OF source_launch_session_id,disc_index ON save_states
WHEN (
  EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND (
    NEW.disc_index IS NULL OR NEW.disc_index >= (
      SELECT count(*) FROM launch_external_files external
      WHERE external.launch_session_id=NEW.source_launch_session_id AND external.kind='DISC'
    )
  )
  OR NOT EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=NEW.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND NEW.disc_index IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'save state disc index mismatch'); END;

CREATE TRIGGER save_states_source_launch_immutable
BEFORE UPDATE OF source_launch_session_id ON save_states
WHEN OLD.source_launch_session_id IS NOT NEW.source_launch_session_id
BEGIN SELECT RAISE(ABORT, 'save state source launch is immutable'); END;

CREATE TRIGGER save_states_source_launch_insert
BEFORE INSERT ON save_states
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1
    FROM launch_sessions launch
    JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
    WHERE launch.id=NEW.source_launch_session_id
    AND launch.purpose='PRODUCT'
    AND launch.profile_id=NEW.profile_id
    AND launch.game_id=NEW.game_id
    AND launch.game_content_revision_id=NEW.game_content_revision_id
    AND launch.game_variant_revision_id=NEW.game_variant_revision_id
    AND launch.core_artifact_id=NEW.core_artifact_id
    AND launch.route_key=revision.route_key
    AND launch.dos_entry_path IS NEW.dos_entry_path
    AND revision.game_content_revision_id=NEW.game_content_revision_id
    AND revision.dat_version_id IS NEW.dat_version_id
  ) THEN RAISE(ABORT, 'save state source launch mismatch') END;
END;

CREATE TRIGGER save_states_published_insert BEFORE INSERT ON save_states
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER save_states_source_launch_update
BEFORE UPDATE OF source_launch_session_id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,
  core_artifact_id,dat_version_id,dos_entry_path
ON save_states
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1
    FROM launch_sessions launch
    JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
    WHERE launch.id=NEW.source_launch_session_id
    AND launch.purpose='PRODUCT'
    AND launch.profile_id=NEW.profile_id
    AND launch.game_id=NEW.game_id
    AND launch.game_content_revision_id=NEW.game_content_revision_id
    AND launch.game_variant_revision_id=NEW.game_variant_revision_id
    AND launch.core_artifact_id=NEW.core_artifact_id
    AND launch.route_key=revision.route_key
    AND launch.dos_entry_path IS NEW.dos_entry_path
    AND revision.game_content_revision_id=NEW.game_content_revision_id
    AND revision.dat_version_id IS NEW.dat_version_id
  ) THEN RAISE(ABORT, 'save state source launch mismatch') END;
END;

CREATE TRIGGER scrape_attempts_immutable_delete BEFORE DELETE ON metadata_scrape_query_attempts BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER scrape_attempts_immutable_update BEFORE UPDATE ON metadata_scrape_query_attempts BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER scrape_candidates_immutable_delete BEFORE DELETE ON scrape_candidates BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER scrape_candidates_immutable_update BEFORE UPDATE ON scrape_candidates BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER scrape_candidate_assets_published_insert BEFORE INSERT ON scrape_candidate_assets
WHEN EXISTS(
  SELECT 1 FROM scrape_candidates candidate
  JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
  JOIN games game ON game.id=run.game_id
  WHERE candidate.id=NEW.scrape_candidate_id AND game.status<>'PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER scrape_candidate_assets_published_update
BEFORE UPDATE OF blob_id ON scrape_candidate_assets
WHEN NEW.blob_id IS NOT NULL AND EXISTS(
  SELECT 1 FROM scrape_candidates candidate
  JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
  JOIN games game ON game.id=run.game_id
  WHERE candidate.id=NEW.scrape_candidate_id AND game.status<>'PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER server_bios_items_frozen_update
BEFORE UPDATE ON server_bios_import_items
WHEN NEW.server_import_id<>OLD.server_import_id OR NEW.requirement_id<>OLD.requirement_id OR
  NEW.requirement_version<>OLD.requirement_version OR NEW.core_id<>OLD.core_id OR
  NEW.core_name_snapshot<>OLD.core_name_snapshot OR NEW.core_artifact_id<>OLD.core_artifact_id OR
  NEW.core_artifact_version<>OLD.core_artifact_version OR NEW.source_kind<>OLD.source_kind OR
  NEW.logical_name<>OLD.logical_name OR NEW.requirement_mode<>OLD.requirement_mode OR
  NEW.condition_code IS NOT OLD.condition_code OR NEW.activation_options_json IS NOT OLD.activation_options_json OR
  NEW.delivery_kind<>OLD.delivery_kind OR NEW.emulator_path IS NOT OLD.emulator_path OR
  NEW.source_version<>OLD.source_version OR NEW.catalog_digest<>OLD.catalog_digest OR
  NEW.dat_version_id IS NOT OLD.dat_version_id OR NEW.dat_machine_name IS NOT OLD.dat_machine_name OR
  NEW.expected_size_bytes IS NOT OLD.expected_size_bytes OR NEW.expected_md5 IS NOT OLD.expected_md5 OR
  NEW.expected_sha1 IS NOT OLD.expected_sha1 OR NEW.expected_sha256 IS NOT OLD.expected_sha256 OR
  NEW.active_installation_id_snapshot IS NOT OLD.active_installation_id_snapshot OR
  NEW.active_installation_version_snapshot IS NOT OLD.active_installation_version_snapshot OR
  NEW.active_blob_sha256_snapshot IS NOT OLD.active_blob_sha256_snapshot OR
  NEW.active_status_snapshot IS NOT OLD.active_status_snapshot OR
  NEW.active_validated_requirement_version_snapshot IS NOT OLD.active_validated_requirement_version_snapshot OR
  NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable server BIOS item snapshot'); END;

CREATE TRIGGER server_bios_items_installation_update
BEFORE UPDATE OF previous_installation_id,new_installation_id ON server_bios_import_items
WHEN (NEW.previous_installation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bios_installations installation
  WHERE installation.id=NEW.previous_installation_id AND installation.requirement_id=NEW.requirement_id
)) OR (NEW.new_installation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bios_installations installation
  WHERE installation.id=NEW.new_installation_id AND installation.requirement_id=NEW.requirement_id
))
BEGIN SELECT RAISE(ABORT,'server BIOS item installation owner mismatch'); END;

CREATE TRIGGER server_import_job_insert
BEFORE INSERT ON server_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.job_id AND job.kind='SERVER_BIOS_IMPORT'
  AND job.scope_type='SERVER_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid server import job'); END;

CREATE TRIGGER tags_guarded_update
BEFORE UPDATE ON tags
WHEN NEW.id<>OLD.id
  OR NEW.created_by_user_id<>OLD.created_by_user_id
  OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.version<>OLD.version+1
  OR NEW.updated_at_ms<OLD.updated_at_ms
  OR OLD.status='DELETED'
  OR (OLD.status='ACTIVE' AND NEW.status NOT IN ('ACTIVE','DELETED'))
  OR (NEW.status='ACTIVE' AND NEW.deleted_at_ms IS NOT NULL)
  OR (NEW.status='DELETED' AND NEW.deleted_at_ms IS NULL)
BEGIN
  SELECT RAISE(ABORT,'invalid tag update');
END;

CREATE TRIGGER tags_no_delete
BEFORE DELETE ON tags
BEGIN
  SELECT RAISE(ABORT,'tag tombstones are immutable');
END;

CREATE TRIGGER users_deleted_terminal
BEFORE UPDATE OF status ON users
WHEN OLD.status='DELETED' AND NEW.status!='DELETED'
BEGIN SELECT RAISE(ABORT, 'deleted user is terminal'); END;

CREATE TRIGGER users_identity_immutable
BEFORE UPDATE OF profile_id,username,created_at_ms ON users
BEGIN SELECT RAISE(ABORT, 'immutable user identity'); END;

CREATE TRIGGER users_last_enabled_admin
BEFORE UPDATE OF role,status ON users
WHEN OLD.role='ADMIN' AND OLD.status='ENABLED' AND
     (NEW.role!='ADMIN' OR NEW.status!='ENABLED')
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM users
    WHERE id!=OLD.id AND role='ADMIN' AND status='ENABLED'
  ) THEN RAISE(ABORT, 'last enabled admin') END;
END;

CREATE TRIGGER users_no_physical_delete
BEFORE DELETE ON users
BEGIN SELECT RAISE(ABORT, 'users are soft deleted'); END;

CREATE TRIGGER variant_dependencies_immutable_delete BEFORE DELETE ON variant_dependencies BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER variant_dependencies_immutable_update BEFORE UPDATE ON variant_dependencies BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER variant_files_immutable_delete
BEFORE DELETE ON variant_files
WHEN NOT EXISTS(
  SELECT 1 FROM game_variant_revisions revision
  JOIN game_variants variant ON variant.id=revision.game_variant_id
  JOIN games game ON game.id=variant.game_id
  WHERE revision.id=OLD.game_variant_revision_id AND (
    game.status='PUBLISHED' AND (
      game.current_content_revision_id<>revision.game_content_revision_id OR
      OLD.role='BIOS_BUNDLE' AND EXISTS(
        SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
        JOIN bios_installations installation
          ON installation.id=json_extract(dependency.value,'$.installationId')
        WHERE installation.payload_released_at_ms IS NOT NULL
          AND json_extract(dependency.value,'$.blobId')=OLD.blob_id
      )
    ) OR
    game.status='DELETED' AND game.payload_state IN ('RELEASING','FAILED')
  )
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER variant_files_immutable_update
BEFORE UPDATE ON variant_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER variant_files_published_insert BEFORE INSERT ON variant_files
WHEN NOT EXISTS(
  SELECT 1 FROM game_variant_revisions revision
  JOIN game_variants variant ON variant.id=revision.game_variant_id
  JOIN games game ON game.id=variant.game_id
  WHERE revision.id=NEW.game_variant_revision_id AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

-- RPG Maker catalog, route, pack, validation and checkpoint invariants.

CREATE UNIQUE INDEX isolated_runtime_ticket_origin
ON isolated_runtime_bootstrap_tickets(expected_origin);

CREATE UNIQUE INDEX isolated_runtime_capability_origin
ON isolated_runtime_capabilities(expected_origin);

CREATE TRIGGER rpgmaker_core_generations_fixed_insert
BEFORE INSERT ON rpgmaker_core_generations
BEGIN SELECT RAISE(ABORT,'RPG Maker core generation catalog is fixed'); END;

CREATE TRIGGER rpgmaker_core_generations_fixed_update
BEFORE UPDATE ON rpgmaker_core_generations
BEGIN SELECT RAISE(ABORT,'RPG Maker core generation catalog is fixed'); END;

CREATE TRIGGER rpgmaker_core_generations_fixed_delete
BEFORE DELETE ON rpgmaker_core_generations
BEGIN SELECT RAISE(ABORT,'RPG Maker core generation catalog is fixed'); END;

CREATE TRIGGER core_artifacts_runtime_insert
BEFORE INSERT ON core_artifacts
WHEN (
  NEW.runtime_family='RPGMAKER'
) <> EXISTS(SELECT 1 FROM rpgmaker_core_generations mapping WHERE mapping.core_id=NEW.core_id)
OR NEW.core_id='onscripter_yuri' AND NEW.runtime_family<>'ONS'
OR NEW.runtime_family='ONS' AND NOT (
  NEW.core_id='onscripter_yuri' AND NEW.runtime_adapter_kind='ONS_YURI_WEB' AND NEW.route_key='ONS_YURI'
)
OR NEW.core_id='kirikiri2' AND NEW.runtime_family<>'KIRIKIRI'
OR NEW.runtime_family='KIRIKIRI' AND NOT (
  NEW.core_id='kirikiri2' AND NEW.runtime_adapter_kind='KIRIKIRI2_WEB' AND NEW.route_key='KIRIKIRI2_KAG'
)
OR NEW.core_id='butterscotch' AND NEW.runtime_family<>'BUTTERSCOTCH'
OR NEW.runtime_family='BUTTERSCOTCH' AND NOT (
  NEW.core_id='butterscotch' AND NEW.runtime_adapter_kind='BUTTERSCOTCH_WEB'
    AND NEW.route_key='BUTTERSCOTCH_GAMEMAKER'
)
OR NEW.core_id='tyranoscript' AND NEW.runtime_family<>'TYRANOSCRIPT'
OR NEW.runtime_family='TYRANOSCRIPT' AND NOT (
  NEW.core_id='tyranoscript' AND NEW.runtime_adapter_kind='TYRANOSCRIPT_WEB'
    AND NEW.route_key='TYRANOSCRIPT_WEB'
)
OR NEW.runtime_family='RPGMAKER' AND NOT (
  NEW.core_id IN ('rpgmaker_2000','rpgmaker_2003')
    AND NEW.runtime_adapter_kind='EASYRPG_WEB'
    AND (
      NEW.core_id='rpgmaker_2000' AND NEW.route_key LIKE 'RPG2000\_%' ESCAPE '\'
      OR NEW.core_id='rpgmaker_2003' AND NEW.route_key LIKE 'RPG2003\_%' ESCAPE '\'
    )
  OR NEW.core_id IN ('rpgmaker_xp','rpgmaker_vx','rpgmaker_vx_ace')
    AND NEW.runtime_adapter_kind='MKXP_LIBRETRO_WEB'
    AND (
      NEW.core_id='rpgmaker_xp' AND NEW.route_key LIKE 'RPGXP\_%' ESCAPE '\'
      OR NEW.core_id='rpgmaker_vx' AND NEW.route_key LIKE 'RPGVX\_%' ESCAPE '\'
      OR NEW.core_id='rpgmaker_vx_ace' AND NEW.route_key LIKE 'RPGVXACE\_%' ESCAPE '\'
    )
  OR NEW.core_id IN ('rpgmaker_mv','rpgmaker_mz')
    AND NEW.runtime_adapter_kind='NATIVE_WEB'
    AND (
      NEW.core_id='rpgmaker_mv' AND NEW.route_key LIKE 'RPGMV\_%' ESCAPE '\'
      OR NEW.core_id='rpgmaker_mz' AND NEW.route_key LIKE 'RPGMZ\_%' ESCAPE '\'
    )
)
BEGIN SELECT RAISE(ABORT,'artifact runtime/core route mismatch'); END;

CREATE TRIGGER core_artifacts_runtime_update
BEFORE UPDATE OF core_id,route_key,runtime_family,runtime_adapter_kind ON core_artifacts
BEGIN SELECT RAISE(ABORT,'artifact runtime identity is immutable'); END;

CREATE TRIGGER core_artifacts_payload_immutable
BEFORE UPDATE OF runtime_version,adapter_id,entry_path,size_bytes,sha256,manifest_sha256,
  artifact_set_sha256,requires_threads,save_payload_kind,save_max_bytes,provenance_json,compatibility_json,created_at_ms
ON core_artifacts
BEGIN SELECT RAISE(ABORT,'artifact payload identity is immutable'); END;

CREATE TRIGGER core_artifacts_availability_update
BEFORE UPDATE OF available_for_launch ON core_artifacts
WHEN OLD.runtime_family='EMULATORJS'
AND OLD.available_for_launch=1 AND NEW.available_for_launch=0 AND (
  EXISTS(SELECT 1 FROM game_variant_revisions revision WHERE revision.core_artifact_id=OLD.id)
  OR EXISTS(SELECT 1 FROM save_states save WHERE save.core_artifact_id=OLD.id AND save.deleted_at_ms IS NULL)
  OR EXISTS(SELECT 1 FROM launch_sessions launch
    WHERE launch.core_artifact_id=OLD.id AND launch.state NOT IN ('FINISHED','EXPIRED','REVOKED'))
  OR EXISTS(SELECT 1 FROM rpgmaker_runtime_validations validation
    WHERE validation.artifact_id=OLD.id AND validation.state NOT IN ('PASSED','FAILED','EXPIRED'))
)
BEGIN SELECT RAISE(ABORT,'referenced artifact must remain available'); END;

CREATE TRIGGER core_artifacts_selected_transition
BEFORE UPDATE OF selected_for_new_bindings,available_for_launch,retired_at_ms ON core_artifacts
WHEN NEW.selected_for_new_bindings=1 AND (
  NEW.available_for_launch<>1 OR NEW.retired_at_ms IS NOT NULL
)
BEGIN SELECT RAISE(ABORT,'selected artifact must be available and current'); END;

CREATE TRIGGER core_artifacts_retirement_immutable
BEFORE UPDATE OF retired_at_ms ON core_artifacts
WHEN OLD.retired_at_ms IS NOT NULL AND NEW.retired_at_ms IS NOT OLD.retired_at_ms
BEGIN SELECT RAISE(ABORT,'artifact retirement is immutable'); END;

CREATE TRIGGER runtime_asset_pack_definitions_immutable_update
BEFORE UPDATE ON runtime_asset_pack_definitions
BEGIN SELECT RAISE(ABORT,'runtime pack definition is immutable'); END;

CREATE TRIGGER runtime_asset_pack_definitions_immutable_delete
BEFORE DELETE ON runtime_asset_pack_definitions
BEGIN SELECT RAISE(ABORT,'runtime pack definition is immutable'); END;

CREATE TRIGGER runtime_asset_pack_installations_blob_insert
BEFORE INSERT ON runtime_asset_pack_installations
WHEN NEW.bundle_blob_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM blobs blob WHERE blob.id=NEW.bundle_blob_id AND blob.sha256=NEW.bundle_sha256
)
BEGIN SELECT RAISE(ABORT,'runtime pack bundle blob mismatch'); END;

CREATE TRIGGER runtime_asset_pack_installations_identity_update
BEFORE UPDATE OF definition_id,files_digest,file_count,total_bytes,source_note,created_by_user_id,created_at_ms
ON runtime_asset_pack_installations
BEGIN SELECT RAISE(ABORT,'runtime pack installation identity is immutable'); END;

CREATE TRIGGER runtime_asset_pack_installations_diagnostic_update
BEFORE UPDATE OF diagnostic_json ON runtime_asset_pack_installations
WHEN OLD.status<>'VALIDATING'
BEGIN SELECT RAISE(ABORT,'terminal runtime pack diagnostic is immutable'); END;

CREATE TRIGGER runtime_asset_pack_installations_version_update
BEFORE UPDATE ON runtime_asset_pack_installations
WHEN NEW.version<>OLD.version+1
BEGIN SELECT RAISE(ABORT,'runtime pack installation version must increment by one'); END;

CREATE TRIGGER runtime_asset_pack_installations_state_update
BEFORE UPDATE OF status,bundle_blob_id,bundle_sha256,validated_at_ms,deleted_at_ms
ON runtime_asset_pack_installations
WHEN NOT (
  OLD.status='VALIDATING' AND NEW.status IN ('READY','FAILED')
    AND NEW.bundle_blob_id IS OLD.bundle_blob_id AND NEW.bundle_sha256 IS OLD.bundle_sha256
    AND (NEW.status<>'READY' OR (
      NEW.bundle_blob_id IS NOT NULL
      AND NEW.file_count=(SELECT count(*) FROM runtime_asset_pack_files file WHERE file.installation_id=OLD.id)
      AND NEW.total_bytes=(SELECT COALESCE(sum(file.size_bytes),0)
        FROM runtime_asset_pack_files file WHERE file.installation_id=OLD.id)
    ))
  OR OLD.status IN ('READY','FAILED') AND NEW.status='DELETE_PENDING'
    AND NEW.bundle_blob_id IS OLD.bundle_blob_id AND NEW.bundle_sha256 IS OLD.bundle_sha256
  OR OLD.status='DELETE_PENDING' AND NEW.status='DELETED'
    AND NEW.bundle_blob_id IS NULL AND NEW.bundle_sha256 IS NULL
    AND NOT EXISTS(SELECT 1 FROM runtime_asset_pack_files file WHERE file.installation_id=OLD.id)
)
OR NEW.status='DELETE_PENDING' AND EXISTS(
  SELECT 1 FROM game_variant_revision_runtime_packs reference
  WHERE reference.installation_id=OLD.id
)
OR NEW.bundle_blob_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM blobs blob WHERE blob.id=NEW.bundle_blob_id AND blob.sha256=NEW.bundle_sha256
)
BEGIN SELECT RAISE(ABORT,'invalid runtime pack installation transition'); END;

CREATE TRIGGER runtime_asset_pack_installations_immutable_delete
BEFORE DELETE ON runtime_asset_pack_installations
BEGIN SELECT RAISE(ABORT,'runtime pack installation is retained for audit'); END;

CREATE TRIGGER runtime_asset_pack_files_insert
BEFORE INSERT ON runtime_asset_pack_files
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_asset_pack_installations installation
  WHERE installation.id=NEW.installation_id AND installation.status='VALIDATING'
)
OR NOT EXISTS(
  SELECT 1 FROM blobs blob
  WHERE blob.id=NEW.blob_id AND blob.size_bytes=NEW.size_bytes AND blob.sha256=NEW.sha256
)
OR NEW.ordinal<>(SELECT count(*) FROM runtime_asset_pack_files file WHERE file.installation_id=NEW.installation_id)
OR NEW.ordinal>0 AND NEW.path<=CAST((
  SELECT file.path FROM runtime_asset_pack_files file
  WHERE file.installation_id=NEW.installation_id AND file.ordinal=NEW.ordinal-1
) AS TEXT)
BEGIN SELECT RAISE(ABORT,'invalid runtime pack file'); END;

CREATE TRIGGER runtime_asset_pack_files_immutable_update
BEFORE UPDATE ON runtime_asset_pack_files
BEGIN SELECT RAISE(ABORT,'runtime pack file is immutable'); END;

CREATE TRIGGER runtime_asset_pack_files_guarded_delete
BEFORE DELETE ON runtime_asset_pack_files
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_asset_pack_installations installation
  WHERE installation.id=OLD.installation_id AND installation.status='DELETE_PENDING'
)
BEGIN SELECT RAISE(ABORT,'runtime pack file is immutable'); END;

CREATE TRIGGER rpgmaker_review_profiles_validate_insert
BEFORE INSERT ON rpgmaker_review_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_items item ON item.id=draft.import_item_id
  JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
  JOIN rpgmaker_core_generations mapping
    ON mapping.core_id=NEW.selected_core_id AND mapping.generation=NEW.generation
  JOIN core_artifacts artifact ON artifact.id=NEW.artifact_id
  WHERE draft.id=NEW.review_draft_id AND item.state='REVIEW_PENDING' AND instance.platform_id='rpgmaker'
    AND instance.default_core_id='rpgmaker'
    AND artifact.core_id=NEW.selected_core_id AND artifact.route_key=NEW.route_key
    AND artifact.runtime_family='RPGMAKER' AND artifact.artifact_set_sha256=NEW.artifact_set_sha256
    AND artifact.adapter_id=NEW.adapter_id
    AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
    AND (NEW.evidence_generation IS NULL OR NEW.evidence_generation=NEW.generation)
)
BEGIN SELECT RAISE(ABORT,'invalid RPG Maker review binding'); END;

CREATE TRIGGER rpgmaker_review_profiles_validate_update
BEFORE UPDATE ON rpgmaker_review_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN import_items item ON item.id=draft.import_item_id
  JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
  JOIN rpgmaker_core_generations mapping
    ON mapping.core_id=NEW.selected_core_id AND mapping.generation=NEW.generation
  JOIN core_artifacts artifact ON artifact.id=NEW.artifact_id
  WHERE draft.id=NEW.review_draft_id AND item.state='REVIEW_PENDING' AND instance.platform_id='rpgmaker'
    AND instance.default_core_id='rpgmaker'
    AND artifact.core_id=NEW.selected_core_id AND artifact.route_key=NEW.route_key
    AND artifact.runtime_family='RPGMAKER' AND artifact.artifact_set_sha256=NEW.artifact_set_sha256
    AND artifact.adapter_id=NEW.adapter_id
    AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
    AND (NEW.evidence_generation IS NULL OR NEW.evidence_generation=NEW.generation)
)
BEGIN SELECT RAISE(ABORT,'invalid RPG Maker review binding'); END;

CREATE TRIGGER review_draft_runtime_pack_selections_validate_insert
BEFORE INSERT ON review_draft_runtime_pack_selections
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_review_profiles profile
  JOIN review_drafts draft ON draft.id=profile.review_draft_id
  JOIN import_items item ON item.id=draft.import_item_id
  JOIN runtime_asset_pack_definitions definition ON definition.id=NEW.definition_id
  JOIN runtime_asset_pack_installations installation
    ON installation.id=NEW.installation_id AND installation.definition_id=definition.id
  WHERE profile.review_draft_id=NEW.review_draft_id AND item.state='REVIEW_PENDING'
    AND definition.enabled=1 AND definition.generation=profile.generation
    AND definition.declared_name=NEW.declared_name
    AND definition.normalized_declared_name=NEW.normalized_declared_name
    AND installation.status='READY'
    AND installation.file_count=(SELECT count(*) FROM runtime_asset_pack_files file
      WHERE file.installation_id=installation.id)
    AND installation.total_bytes=(SELECT COALESCE(sum(file.size_bytes),0) FROM runtime_asset_pack_files file
      WHERE file.installation_id=installation.id)
    AND (
      profile.generation IN ('RPG2000','RPG2003') AND NEW.slot=0
      OR profile.generation IN ('RPGXP','RPGVX','RPGVXACE') AND NEW.slot BETWEEN 1 AND 3
    )
)
BEGIN SELECT RAISE(ABORT,'invalid review runtime pack selection'); END;

CREATE TRIGGER review_draft_runtime_pack_selections_validate_update
BEFORE UPDATE ON review_draft_runtime_pack_selections
BEGIN SELECT RAISE(ABORT,'replace runtime pack selection atomically'); END;

CREATE TRIGGER review_draft_runtime_pack_selections_validate_delete
BEFORE DELETE ON review_draft_runtime_pack_selections
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
  WHERE draft.id=OLD.review_draft_id AND item.state='REVIEW_PENDING'
)
BEGIN SELECT RAISE(ABORT,'finalized review runtime pack selection'); END;

CREATE TRIGGER rpgmaker_content_profiles_validate_insert
BEFORE INSERT ON rpgmaker_content_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM game_content_revisions revision
  WHERE revision.id=NEW.content_revision_id AND revision.content_kind='RPG_MAKER_PROJECT_V1'
    AND json_extract(revision.source_manifest_json,'$.fileCount')=NEW.file_count
    AND json_extract(revision.source_manifest_json,'$.totalBytes')=NEW.total_bytes
    AND json_extract(revision.source_manifest_json,'$.filesDigest')=NEW.project_fingerprint
)
BEGIN SELECT RAISE(ABORT,'RPG Maker content profile manifest mismatch'); END;

CREATE TRIGGER rpgmaker_content_profiles_immutable_update
BEFORE UPDATE ON rpgmaker_content_profiles
BEGIN SELECT RAISE(ABORT,'RPG Maker content profile is immutable'); END;

CREATE TRIGGER rpgmaker_content_profiles_immutable_delete
BEFORE DELETE ON rpgmaker_content_profiles
BEGIN SELECT RAISE(ABORT,'RPG Maker content profile is immutable'); END;

CREATE TRIGGER game_variant_revisions_runtime_insert
BEFORE INSERT ON game_variant_revisions
WHEN NOT EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN core_artifacts artifact ON artifact.id=NEW.core_artifact_id
  WHERE variant.id=NEW.game_variant_id AND artifact.core_id=variant.core_id
    AND artifact.route_key=NEW.route_key AND artifact.available_for_launch=1
    AND (
      artifact.runtime_family='EMULATORJS' AND NEW.emulator_game_id IS NOT NULL
      OR artifact.runtime_family='RPGMAKER' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='ONS' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='KIRIKIRI' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='BUTTERSCOTCH' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='TYRANOSCRIPT' AND NEW.emulator_game_id IS NULL
    )
)
BEGIN SELECT RAISE(ABORT,'variant revision runtime mismatch'); END;

CREATE TRIGGER rpgmaker_variant_profiles_validate_insert
BEFORE INSERT ON rpgmaker_variant_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM game_variant_revisions revision
  JOIN game_variants variant ON variant.id=revision.game_variant_id
  JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
  JOIN rpgmaker_core_generations mapping
    ON mapping.core_id=variant.core_id AND mapping.generation=NEW.generation
  JOIN rpgmaker_content_profiles content ON content.content_revision_id=revision.game_content_revision_id
  JOIN rpgmaker_runtime_validations validation ON validation.id=NEW.runtime_validation_id
  WHERE revision.id=NEW.game_variant_revision_id AND revision.status='READY'
    AND artifact.runtime_family='RPGMAKER' AND artifact.route_key=revision.route_key
    AND artifact.available_for_launch=1
    AND NEW.route_key=revision.route_key AND NEW.adapter_id=artifact.adapter_id
    AND NEW.artifact_set_sha256=artifact.artifact_set_sha256
    AND (content.evidence_generation=NEW.generation OR
      content.evidence_family='RPG2K' AND content.evidence_confidence='FAMILY_ONLY'
        AND content.evidence_generation IS NULL AND NEW.generation IN ('RPG2000','RPG2003'))
    AND validation.state='PASSED' AND validation.core_id=variant.core_id
    AND validation.generation=NEW.generation AND validation.route_key=NEW.route_key
    AND validation.artifact_id=artifact.id
    AND validation.artifact_set_sha256=NEW.artifact_set_sha256
    AND validation.adapter_id=NEW.adapter_id AND validation.adapter_abi=NEW.adapter_abi
    AND validation.dependency_snapshot_sha256=NEW.dependency_snapshot_sha256
    AND validation.project_fingerprint=content.project_fingerprint
)
BEGIN SELECT RAISE(ABORT,'invalid RPG Maker variant profile'); END;

CREATE TRIGGER rpgmaker_variant_profiles_immutable_update
BEFORE UPDATE ON rpgmaker_variant_profiles
BEGIN SELECT RAISE(ABORT,'RPG Maker variant profile is immutable'); END;

CREATE TRIGGER rpgmaker_variant_profiles_immutable_delete
BEFORE DELETE ON rpgmaker_variant_profiles
BEGIN SELECT RAISE(ABORT,'RPG Maker variant profile is immutable'); END;

CREATE TRIGGER game_variant_revision_runtime_packs_validate_insert
BEFORE INSERT ON game_variant_revision_runtime_packs
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_variant_profiles profile
  JOIN runtime_asset_pack_definitions definition ON definition.id=NEW.definition_id
  JOIN runtime_asset_pack_installations installation
    ON installation.id=NEW.installation_id AND installation.definition_id=definition.id
  WHERE profile.game_variant_revision_id=NEW.game_variant_revision_id
    AND definition.enabled=1 AND definition.generation=profile.generation
    AND definition.declared_name=NEW.declared_name
    AND definition.normalized_declared_name=NEW.normalized_declared_name
    AND installation.status='READY'
    AND installation.file_count=(SELECT count(*) FROM runtime_asset_pack_files file
      WHERE file.installation_id=installation.id)
    AND installation.total_bytes=(SELECT COALESCE(sum(file.size_bytes),0) FROM runtime_asset_pack_files file
      WHERE file.installation_id=installation.id)
    AND (
      profile.generation IN ('RPG2000','RPG2003') AND NEW.slot=0
      OR profile.generation IN ('RPGXP','RPGVX','RPGVXACE') AND NEW.slot BETWEEN 1 AND 3
    )
)
BEGIN SELECT RAISE(ABORT,'invalid variant runtime pack'); END;

CREATE TRIGGER game_variant_revision_runtime_packs_immutable_update
BEFORE UPDATE ON game_variant_revision_runtime_packs
BEGIN SELECT RAISE(ABORT,'variant runtime pack is immutable'); END;

CREATE TRIGGER game_variant_revision_runtime_packs_immutable_delete
BEFORE DELETE ON game_variant_revision_runtime_packs
BEGIN SELECT RAISE(ABORT,'variant runtime pack is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validations_validate_insert
BEFORE INSERT ON rpgmaker_runtime_validations
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft
  JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
  JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
  JOIN core_artifacts artifact ON artifact.id=NEW.artifact_id
  JOIN rpgmaker_core_generations mapping
    ON mapping.core_id=NEW.core_id AND mapping.generation=NEW.generation
  WHERE draft.import_item_id=NEW.import_item_id
    AND draft.version=NEW.review_version_at_create
    AND draft.runtime_binding_revision=NEW.runtime_binding_revision
    AND snapshot.id=NEW.effective_source_snapshot_id
    AND snapshot.import_item_id=NEW.import_item_id
    AND profile.selected_core_id=NEW.core_id AND profile.generation=NEW.generation
    AND profile.project_fingerprint=NEW.project_fingerprint
    AND profile.evidence_generation IS NEW.evidence_generation
    AND profile.evidence_confidence=NEW.evidence_confidence
    AND profile.route_key=NEW.route_key AND profile.artifact_id=NEW.artifact_id
    AND profile.artifact_set_sha256=NEW.artifact_set_sha256
    AND profile.adapter_id=NEW.adapter_id AND profile.adapter_abi=NEW.adapter_abi
    AND profile.dependency_snapshot_sha256=NEW.dependency_snapshot_sha256
    AND artifact.core_id=NEW.core_id AND artifact.route_key=NEW.route_key
    AND artifact.artifact_set_sha256=NEW.artifact_set_sha256
    AND artifact.adapter_id=NEW.adapter_id AND artifact.runtime_family='RPGMAKER'
    AND artifact.selected_for_new_bindings=1 AND artifact.available_for_launch=1
)
BEGIN SELECT RAISE(ABORT,'invalid RPG Maker runtime validation binding'); END;

CREATE TRIGGER rpgmaker_runtime_validations_binding_immutable
BEFORE UPDATE OF import_item_id,review_version_at_create,runtime_binding_revision,effective_source_snapshot_id,
  project_fingerprint,core_id,generation,evidence_generation,evidence_confidence,route_key,artifact_id,
  artifact_set_sha256,adapter_id,adapter_abi,dependency_snapshot_sha256,created_at_ms,expires_at_ms
ON rpgmaker_runtime_validations
BEGIN SELECT RAISE(ABORT,'runtime validation binding is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validations_transition
BEFORE UPDATE OF state ON rpgmaker_runtime_validations
WHEN NEW.state<>OLD.state AND NOT (
  OLD.state='CREATED' AND NEW.state IN ('STARTING','FAILED','EXPIRED')
  OR OLD.state='STARTING' AND NEW.state IN ('RUNNING','FAILED','EXPIRED')
  OR OLD.state='RUNNING' AND NEW.state IN ('CHECKPOINTED','FAILED','EXPIRED')
  OR OLD.state='CHECKPOINTED' AND NEW.state IN ('RESTORED','FAILED','EXPIRED')
  OR OLD.state='RESTORED' AND NEW.state IN ('AWAITING_DECISION','FAILED','EXPIRED')
  OR OLD.state='AWAITING_DECISION' AND NEW.state IN ('PASSED','FAILED','EXPIRED')
)
BEGIN SELECT RAISE(ABORT,'invalid runtime validation transition'); END;

CREATE TRIGGER rpgmaker_runtime_validations_transition_evidence
BEFORE UPDATE OF state ON rpgmaker_runtime_validations
WHEN NEW.state='CHECKPOINTED' AND (
  NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_checkpoints checkpoint
    WHERE checkpoint.validation_id=NEW.id)
  OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=NEW.id AND event.gate='CHECKPOINT_CREATED' AND event.phase='PASS')
)
OR NEW.state='RESTORED' AND (
  NEW.restore_launch_id IS NULL
  OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=NEW.id AND event.gate='RESTORE_POSITION_VERIFIED' AND event.phase='PASS')
)
OR NEW.state='AWAITING_DECISION' AND (
  NEW.evidence_screenshot_blob_id IS NULL
  OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=NEW.id AND event.gate='RESTORE_SCREENSHOT' AND event.phase='PASS')
  OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=NEW.id AND event.gate='RESTORE_INPUT' AND event.phase='PASS')
)
BEGIN SELECT RAISE(ABORT,'runtime validation transition evidence is incomplete'); END;

CREATE TRIGGER rpgmaker_runtime_validations_terminal_immutable
BEFORE UPDATE ON rpgmaker_runtime_validations
WHEN OLD.state IN ('PASSED','FAILED','EXPIRED')
BEGIN SELECT RAISE(ABORT,'terminal runtime validation is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validations_launch_update
BEFORE UPDATE OF launch_id,restore_launch_id ON rpgmaker_runtime_validations
WHEN NEW.launch_id IS NOT OLD.launch_id OR NEW.restore_launch_id IS NOT OLD.restore_launch_id
BEGIN
  SELECT CASE WHEN OLD.launch_id IS NOT NULL AND NEW.launch_id IS NOT OLD.launch_id
    THEN RAISE(ABORT,'validation launch is immutable') END;
  SELECT CASE WHEN OLD.restore_launch_id IS NOT NULL AND NEW.restore_launch_id IS NOT OLD.restore_launch_id
    THEN RAISE(ABORT,'validation restore launch is immutable') END;
  SELECT CASE WHEN NEW.launch_id IS NOT NULL AND NOT EXISTS(
    SELECT 1 FROM launch_sessions launch
    WHERE launch.id=NEW.launch_id AND launch.purpose='RPG_RUNTIME_VALIDATION'
      AND launch.rpgmaker_runtime_validation_id=NEW.id
      AND launch.effective_source_snapshot_id=NEW.effective_source_snapshot_id
      AND launch.core_artifact_id=NEW.artifact_id AND launch.route_key=NEW.route_key
  ) THEN RAISE(ABORT,'invalid validation launch') END;
  SELECT CASE WHEN NEW.restore_launch_id IS NOT NULL AND (
    NEW.launch_id IS NULL OR NEW.restore_launch_id=NEW.launch_id OR OLD.state<>'CHECKPOINTED'
    OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_checkpoints checkpoint
      WHERE checkpoint.validation_id=NEW.id)
    OR NOT EXISTS(SELECT 1 FROM launch_sessions original
      WHERE original.id=NEW.launch_id AND original.state IN ('FINISHED','EXPIRED','REVOKED'))
    OR NOT EXISTS(SELECT 1 FROM launch_sessions restore
      WHERE restore.id=NEW.restore_launch_id AND restore.purpose='RPG_RUNTIME_VALIDATION'
        AND restore.rpgmaker_runtime_validation_id=NEW.id
        AND restore.effective_source_snapshot_id=NEW.effective_source_snapshot_id
        AND restore.core_artifact_id=NEW.artifact_id AND restore.route_key=NEW.route_key)
  ) THEN RAISE(ABORT,'invalid validation restore launch') END;
END;

CREATE TRIGGER rpgmaker_runtime_validations_pass
BEFORE UPDATE OF state ON rpgmaker_runtime_validations
WHEN NEW.state='PASSED'
BEGIN
  SELECT CASE WHEN NEW.restore_launch_id IS NULL OR NEW.restore_launch_id=NEW.launch_id
    OR NEW.evidence_screenshot_blob_id IS NULL
    OR EXISTS(
      SELECT required.gate FROM (
        SELECT 'RUNTIME_READY' AS gate UNION ALL SELECT 'ENGINE_PROFILE'
        UNION ALL SELECT 'FRAMES_300' UNION ALL SELECT 'INPUT' UNION ALL SELECT 'AUDIO'
        UNION ALL SELECT 'INITIAL_POSITION_RECORDED'
        UNION ALL SELECT 'SAVE_POINT_RECORDED' UNION ALL SELECT 'CHECKPOINT_CREATED'
        UNION ALL SELECT 'POST_SAVE_STATE_DIVERGED' UNION ALL SELECT 'ORIGINAL_LAUNCH_ENDED'
        UNION ALL SELECT 'RESTORE_STARTED' UNION ALL SELECT 'RESTORE_POSITION_VERIFIED'
        UNION ALL SELECT 'RESTORE_SCREENSHOT' UNION ALL SELECT 'RESTORE_INPUT'
      ) required
      WHERE NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
        WHERE event.validation_id=NEW.id AND event.gate=required.gate AND event.phase='PASS')
    )
  THEN RAISE(ABORT,'runtime validation gates are incomplete') END;
END;

CREATE TRIGGER rpgmaker_runtime_validation_gate_events_insert
BEFORE INSERT ON rpgmaker_runtime_validation_gate_events
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validations validation
  WHERE validation.id=NEW.validation_id
    AND validation.state NOT IN ('PASSED','FAILED','EXPIRED')
    AND NEW.sequence=validation.last_gate_sequence+1
    AND (
      NEW.gate IN (
        'RUNTIME_READY','ENGINE_PROFILE','FRAMES_300','INPUT','AUDIO','INITIAL_POSITION_RECORDED',
        'SAVE_POINT_RECORDED',
        'CHECKPOINT_CREATED','POST_SAVE_STATE_DIVERGED','ORIGINAL_LAUNCH_ENDED'
      ) AND NEW.launch_id=validation.launch_id
      OR NEW.gate IN ('RESTORE_STARTED','RESTORE_POSITION_VERIFIED','RESTORE_SCREENSHOT','RESTORE_INPUT')
        AND NEW.launch_id=validation.restore_launch_id
    )
)
OR NEW.phase='BEGIN' AND (
  EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events current
    WHERE current.validation_id=NEW.validation_id AND current.gate=NEW.gate)
  OR NOT CASE NEW.gate
    WHEN 'RUNTIME_READY' THEN NOT EXISTS(
      SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id)
    WHEN 'ENGINE_PROFILE' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='RUNTIME_READY' AND prior.phase='PASS')
    WHEN 'FRAMES_300' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='ENGINE_PROFILE' AND prior.phase='PASS')
    WHEN 'INPUT' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='FRAMES_300' AND prior.phase='PASS')
    WHEN 'AUDIO' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='INPUT' AND prior.phase='PASS')
    WHEN 'INITIAL_POSITION_RECORDED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='AUDIO' AND prior.phase='PASS')
    WHEN 'SAVE_POINT_RECORDED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='INITIAL_POSITION_RECORDED' AND prior.phase='PASS')
    WHEN 'CHECKPOINT_CREATED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='SAVE_POINT_RECORDED' AND prior.phase='PASS')
    WHEN 'POST_SAVE_STATE_DIVERGED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='CHECKPOINT_CREATED' AND prior.phase='PASS')
    WHEN 'ORIGINAL_LAUNCH_ENDED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='POST_SAVE_STATE_DIVERGED' AND prior.phase='PASS')
    WHEN 'RESTORE_STARTED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='ORIGINAL_LAUNCH_ENDED' AND prior.phase='PASS')
    WHEN 'RESTORE_POSITION_VERIFIED' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='RESTORE_STARTED' AND prior.phase='PASS')
    WHEN 'RESTORE_SCREENSHOT' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='RESTORE_POSITION_VERIFIED' AND prior.phase='PASS')
    WHEN 'RESTORE_INPUT' THEN EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events prior
      WHERE prior.validation_id=NEW.validation_id AND prior.gate='RESTORE_SCREENSHOT' AND prior.phase='PASS')
    ELSE 0
  END
)
OR NEW.phase IN ('PASS','FAIL') AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validation_gate_events begun
  WHERE begun.validation_id=NEW.validation_id AND begun.gate=NEW.gate AND begun.phase='BEGIN'
    AND NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events terminal
      WHERE terminal.validation_id=NEW.validation_id AND terminal.gate=NEW.gate
        AND terminal.phase IN ('PASS','FAIL'))
)
OR NEW.gate IN (
    'INITIAL_POSITION_RECORDED','SAVE_POINT_RECORDED','POST_SAVE_STATE_DIVERGED',
    'RESTORE_POSITION_VERIFIED','RESTORE_INPUT'
  )
  AND NEW.phase='PASS' AND NOT (
    json_type(NEW.evidence_json)='object'
    AND (SELECT count(*) FROM json_each(NEW.evidence_json))=4
    AND json_type(NEW.evidence_json,'$.mapId')='integer'
    AND json_type(NEW.evidence_json,'$.playerX')='integer'
    AND json_type(NEW.evidence_json,'$.playerY')='integer'
    AND json_type(NEW.evidence_json,'$.fixtureState')='integer'
    AND json_extract(NEW.evidence_json,'$.mapId') BETWEEN 1 AND 2147483647
    AND json_extract(NEW.evidence_json,'$.playerX') BETWEEN 0 AND 2147483647
    AND json_extract(NEW.evidence_json,'$.playerY') BETWEEN 0 AND 2147483647
    AND json_extract(NEW.evidence_json,'$.fixtureState') BETWEEN -2147483648 AND 2147483647
  )
OR NEW.gate='SAVE_POINT_RECORDED' AND NEW.phase='PASS' AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validation_gate_events initial
  WHERE initial.validation_id=NEW.validation_id
    AND initial.gate='INITIAL_POSITION_RECORDED' AND initial.phase='PASS'
    AND NOT (
      json_extract(initial.evidence_json,'$.mapId')=json_extract(NEW.evidence_json,'$.mapId')
      AND json_extract(initial.evidence_json,'$.playerX')=json_extract(NEW.evidence_json,'$.playerX')
      AND json_extract(initial.evidence_json,'$.playerY')=json_extract(NEW.evidence_json,'$.playerY')
      AND json_extract(initial.evidence_json,'$.fixtureState')=json_extract(NEW.evidence_json,'$.fixtureState')
    )
)
OR NEW.gate='POST_SAVE_STATE_DIVERGED' AND NEW.phase='PASS' AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validation_gate_events saved
  WHERE saved.validation_id=NEW.validation_id AND saved.gate='SAVE_POINT_RECORDED' AND saved.phase='PASS'
    AND NOT (
      json_extract(saved.evidence_json,'$.mapId')=json_extract(NEW.evidence_json,'$.mapId')
      AND json_extract(saved.evidence_json,'$.playerX')=json_extract(NEW.evidence_json,'$.playerX')
      AND json_extract(saved.evidence_json,'$.playerY')=json_extract(NEW.evidence_json,'$.playerY')
      AND json_extract(saved.evidence_json,'$.fixtureState')=json_extract(NEW.evidence_json,'$.fixtureState')
    )
)
OR NEW.gate='RESTORE_POSITION_VERIFIED' AND NEW.phase='PASS' AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validation_gate_events initial
  JOIN rpgmaker_runtime_validation_gate_events saved
    ON saved.validation_id=initial.validation_id
    AND saved.gate='SAVE_POINT_RECORDED' AND saved.phase='PASS'
  JOIN rpgmaker_runtime_validation_gate_events diverged
    ON diverged.validation_id=saved.validation_id
    AND diverged.gate='POST_SAVE_STATE_DIVERGED' AND diverged.phase='PASS'
  WHERE initial.validation_id=NEW.validation_id
    AND initial.gate='INITIAL_POSITION_RECORDED' AND initial.phase='PASS'
    AND json_extract(saved.evidence_json,'$.mapId')=json_extract(NEW.evidence_json,'$.mapId')
    AND json_extract(saved.evidence_json,'$.playerX')=json_extract(NEW.evidence_json,'$.playerX')
    AND json_extract(saved.evidence_json,'$.playerY')=json_extract(NEW.evidence_json,'$.playerY')
    AND json_extract(saved.evidence_json,'$.fixtureState')=json_extract(NEW.evidence_json,'$.fixtureState')
    AND NOT (
      json_extract(diverged.evidence_json,'$.mapId')=json_extract(NEW.evidence_json,'$.mapId')
      AND json_extract(diverged.evidence_json,'$.playerX')=json_extract(NEW.evidence_json,'$.playerX')
      AND json_extract(diverged.evidence_json,'$.playerY')=json_extract(NEW.evidence_json,'$.playerY')
      AND json_extract(diverged.evidence_json,'$.fixtureState')=json_extract(NEW.evidence_json,'$.fixtureState')
    )
    AND NOT (
      json_extract(initial.evidence_json,'$.mapId')=json_extract(NEW.evidence_json,'$.mapId')
      AND json_extract(initial.evidence_json,'$.playerX')=json_extract(NEW.evidence_json,'$.playerX')
      AND json_extract(initial.evidence_json,'$.playerY')=json_extract(NEW.evidence_json,'$.playerY')
      AND json_extract(initial.evidence_json,'$.fixtureState')=json_extract(NEW.evidence_json,'$.fixtureState')
    )
)
OR NEW.gate='RESTORE_INPUT' AND NEW.phase='PASS' AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validation_gate_events restored
  WHERE restored.validation_id=NEW.validation_id
    AND restored.gate='RESTORE_POSITION_VERIFIED' AND restored.phase='PASS'
    AND NOT (
      json_extract(restored.evidence_json,'$.mapId')=json_extract(NEW.evidence_json,'$.mapId')
      AND json_extract(restored.evidence_json,'$.playerX')=json_extract(NEW.evidence_json,'$.playerX')
      AND json_extract(restored.evidence_json,'$.playerY')=json_extract(NEW.evidence_json,'$.playerY')
      AND json_extract(restored.evidence_json,'$.fixtureState')=json_extract(NEW.evidence_json,'$.fixtureState')
    )
)
BEGIN SELECT RAISE(ABORT,'invalid runtime validation gate event'); END;

CREATE TRIGGER rpgmaker_runtime_validation_gate_events_immutable_update
BEFORE UPDATE ON rpgmaker_runtime_validation_gate_events
BEGIN SELECT RAISE(ABORT,'runtime validation gate evidence is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validation_gate_events_immutable_delete
BEFORE DELETE ON rpgmaker_runtime_validation_gate_events
BEGIN SELECT RAISE(ABORT,'runtime validation gate evidence is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validations_gate_sequence_update
BEFORE UPDATE OF last_gate_sequence ON rpgmaker_runtime_validations
WHEN NEW.last_gate_sequence<>OLD.last_gate_sequence AND (
  NEW.last_gate_sequence<>OLD.last_gate_sequence+1
  OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=NEW.id AND event.sequence=NEW.last_gate_sequence)
)
BEGIN SELECT RAISE(ABORT,'runtime validation gate sequence mismatch'); END;

CREATE TRIGGER launch_sessions_runtime_binding_insert
BEFORE INSERT ON launch_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM core_artifacts artifact
  WHERE artifact.id=NEW.core_artifact_id AND artifact.route_key=NEW.route_key
    AND artifact.available_for_launch=1
)
OR NEW.purpose='PRODUCT' AND NOT EXISTS(
  SELECT 1 FROM games game
  JOIN game_variant_revisions revision ON revision.id=NEW.game_variant_revision_id
  JOIN game_variants variant ON variant.id=revision.game_variant_id
  JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
  JOIN core_artifacts launch_artifact ON launch_artifact.id=NEW.core_artifact_id
  WHERE game.id=NEW.game_id AND game.status='PUBLISHED'
    AND variant.game_id=game.id AND revision.game_content_revision_id=NEW.game_content_revision_id
    AND revision.status='READY'
    AND (
      launch_artifact.runtime_family='EMULATORJS'
        AND revision.core_artifact_id=NEW.core_artifact_id AND revision.route_key=NEW.route_key
      OR launch_artifact.runtime_family IN ('RPGMAKER','ONS','KIRIKIRI','BUTTERSCOTCH','TYRANOSCRIPT')
        AND launch_artifact.selected_for_new_bindings=1
        AND launch_artifact.core_id=bound_artifact.core_id
        AND launch_artifact.runtime_family=bound_artifact.runtime_family
        AND launch_artifact.route_key=revision.route_key AND revision.route_key=NEW.route_key
        AND json_extract(launch_artifact.compatibility_json,'$.gameCompatibilityLine')=
            json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
    )
)
OR NEW.purpose='PRODUCT' AND NEW.save_state_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM save_states save
  JOIN core_artifacts writer ON writer.id=save.core_artifact_id
  JOIN core_artifacts launch_artifact ON launch_artifact.id=NEW.core_artifact_id
  JOIN save_state_runtime_compatibility compatibility ON compatibility.save_state_id=save.id
  WHERE save.id=NEW.save_state_id AND save.deleted_at_ms IS NULL
    AND save.profile_id=NEW.profile_id AND save.game_id=NEW.game_id
    AND save.game_content_revision_id=NEW.game_content_revision_id
    AND save.game_variant_revision_id=NEW.game_variant_revision_id
    AND (
      launch_artifact.runtime_family='EMULATORJS' AND save.core_artifact_id=NEW.core_artifact_id
      OR launch_artifact.runtime_family IN ('RPGMAKER','ONS','KIRIKIRI','BUTTERSCOTCH','TYRANOSCRIPT')
        AND compatibility.status='AVAILABLE'
        AND launch_artifact.selected_for_new_bindings=1
        AND launch_artifact.core_id=writer.core_id
        AND launch_artifact.runtime_family=writer.runtime_family
        AND launch_artifact.route_key=writer.route_key
    )
)
OR NEW.purpose='RPG_RUNTIME_VALIDATION' AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validations validation
  WHERE validation.id=NEW.rpgmaker_runtime_validation_id
    AND validation.effective_source_snapshot_id=NEW.effective_source_snapshot_id
    AND validation.artifact_id=NEW.core_artifact_id AND validation.route_key=NEW.route_key
    AND (
      validation.launch_id IS NULL AND validation.state IN ('CREATED','STARTING')
      OR validation.launch_id IS NOT NULL AND validation.restore_launch_id IS NULL
        AND validation.state='CHECKPOINTED'
        AND EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_checkpoints checkpoint
          WHERE checkpoint.validation_id=validation.id)
        AND EXISTS(SELECT 1 FROM launch_sessions original
          WHERE original.id=validation.launch_id AND original.state IN ('FINISHED','EXPIRED','REVOKED'))
    )
)
BEGIN SELECT RAISE(ABORT,'invalid launch runtime binding'); END;

CREATE TRIGGER launch_sessions_runtime_binding_immutable
BEFORE UPDATE OF purpose,profile_id,game_id,game_content_revision_id,game_variant_revision_id,
  core_artifact_id,route_key,effective_source_snapshot_id,rpgmaker_runtime_validation_id,save_state_id,
  dos_entry_path,credential_sha256,created_at_ms
ON launch_sessions
BEGIN SELECT RAISE(ABORT,'launch runtime binding is immutable'); END;

CREATE TRIGGER launch_sessions_revoke_isolated_runtime
AFTER UPDATE OF state ON launch_sessions
WHEN NEW.state IN ('FINISHED','EXPIRED','REVOKED') AND OLD.state<>NEW.state
BEGIN
  UPDATE isolated_runtime_capabilities SET revoked_at_ms=NEW.finished_at_ms
  WHERE launch_id=NEW.id AND revoked_at_ms IS NULL;
END;

CREATE TRIGGER review_preview_sessions_revoke_isolated_runtime
AFTER UPDATE OF state ON review_preview_sessions
WHEN NEW.state IN ('EXPIRED','REVOKED') AND OLD.state<>NEW.state
BEGIN
  UPDATE isolated_runtime_capabilities SET revoked_at_ms=NEW.finished_at_ms
  WHERE preview_id=NEW.id AND revoked_at_ms IS NULL;
END;

CREATE TRIGGER rpgmaker_runtime_validation_checkpoints_insert
BEFORE INSERT ON rpgmaker_runtime_validation_checkpoints
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validations validation
  JOIN launch_sessions launch ON launch.id=validation.launch_id
  JOIN core_artifacts artifact ON artifact.id=validation.artifact_id
  JOIN blobs blob ON blob.id=NEW.payload_blob_id
  WHERE validation.id=NEW.validation_id AND validation.state='RUNNING'
    AND launch.purpose='RPG_RUNTIME_VALIDATION'
    AND launch.rpgmaker_runtime_validation_id=validation.id
    AND artifact.save_payload_kind=NEW.payload_kind
    AND NEW.size_bytes<=artifact.save_max_bytes
    AND blob.sha256=NEW.payload_sha256 AND blob.size_bytes=NEW.size_bytes
    AND (
      NEW.payload_kind='RUNTIME_STATE'
        AND validation.generation IN ('RPGXP','RPGVX','RPGVXACE')
      OR NEW.payload_kind='NATIVE_SAVE_BUNDLE_V1' AND (
        validation.generation IN ('RPG2000','RPG2003')
          AND NEW.native_profile='EASYRPG_V1' AND NEW.resume_slot=100
        OR validation.generation='RPGMV' AND NEW.native_profile='RPGMV_V1'
        OR validation.generation='RPGMZ' AND NEW.native_profile='RPGMZ_V1'
      )
    )
)
BEGIN SELECT RAISE(ABORT,'invalid runtime validation checkpoint'); END;

CREATE TRIGGER rpgmaker_runtime_validation_checkpoints_immutable_update
BEFORE UPDATE ON rpgmaker_runtime_validation_checkpoints
BEGIN SELECT RAISE(ABORT,'runtime validation checkpoint is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validation_checkpoints_guarded_delete
BEFORE DELETE ON rpgmaker_runtime_validation_checkpoints
WHEN NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validations validation
  WHERE validation.id=OLD.validation_id AND validation.state IN ('PASSED','FAILED','EXPIRED'))
BEGIN SELECT RAISE(ABORT,'active runtime validation checkpoint is immutable'); END;

CREATE TRIGGER save_states_payload_insert
BEFORE INSERT ON save_states
WHEN NOT EXISTS(
  SELECT 1 FROM core_artifacts artifact
  JOIN blobs blob ON blob.id=NEW.payload_blob_id
  WHERE artifact.id=NEW.core_artifact_id AND artifact.available_for_launch=1
    AND artifact.save_payload_kind=NEW.payload_kind
    AND NEW.payload_size_bytes<=artifact.save_max_bytes
    AND blob.sha256=NEW.payload_sha256 AND blob.size_bytes=NEW.payload_size_bytes
    AND json_extract(artifact.compatibility_json,'$.adapterAbi')=NEW.adapter_abi
    AND COALESCE(json_extract(artifact.compatibility_json,'$.saveAbi'),
                 json_extract(artifact.compatibility_json,'$.adapterAbi'))=NEW.save_abi
)
OR NOT EXISTS(
  SELECT 1 FROM game_variant_revisions revision
  JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
  JOIN core_artifacts writer ON writer.id=NEW.core_artifact_id
  WHERE revision.id=NEW.game_variant_revision_id
    AND revision.game_content_revision_id=NEW.game_content_revision_id
    AND (
      writer.runtime_family='EMULATORJS' AND revision.core_artifact_id=NEW.core_artifact_id
      OR writer.runtime_family='ONS'
        AND writer.core_id=bound_artifact.core_id AND writer.route_key=revision.route_key
        AND json_extract(writer.compatibility_json,'$.gameCompatibilityLine')=
            json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
      OR writer.runtime_family='KIRIKIRI'
        AND writer.core_id=bound_artifact.core_id AND writer.route_key=revision.route_key
        AND json_extract(writer.compatibility_json,'$.gameCompatibilityLine')=
            json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
      OR writer.runtime_family='BUTTERSCOTCH'
        AND writer.core_id=bound_artifact.core_id AND writer.route_key=revision.route_key
        AND json_extract(writer.compatibility_json,'$.gameCompatibilityLine')=
            json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
      OR writer.runtime_family='TYRANOSCRIPT'
        AND writer.core_id=bound_artifact.core_id AND writer.route_key=revision.route_key
        AND json_extract(writer.compatibility_json,'$.gameCompatibilityLine')=
            json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
      OR writer.runtime_family='RPGMAKER'
        AND writer.core_id=bound_artifact.core_id AND writer.route_key=revision.route_key
        AND json_extract(writer.compatibility_json,'$.gameCompatibilityLine')=
            json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
        AND EXISTS(SELECT 1 FROM rpgmaker_variant_profiles profile
        WHERE profile.game_variant_revision_id=revision.id
          AND profile.dependency_snapshot_sha256=NEW.dependency_snapshot_sha256
          AND (
            NEW.payload_kind='RUNTIME_STATE' AND profile.generation IN ('RPGXP','RPGVX','RPGVXACE')
            OR NEW.payload_kind='NATIVE_SAVE_BUNDLE_V1' AND (
              profile.generation IN ('RPG2000','RPG2003')
                AND NEW.native_profile='EASYRPG_V1' AND NEW.resume_slot=100
              OR profile.generation='RPGMV' AND NEW.native_profile='RPGMV_V1'
              OR profile.generation='RPGMZ' AND NEW.native_profile='RPGMZ_V1'
            )
          )
      )
    )
)
BEGIN SELECT RAISE(ABORT,'save checkpoint payload mismatch'); END;

CREATE TRIGGER save_states_payload_immutable
BEFORE UPDATE OF profile_id,game_id,game_content_revision_id,game_variant_revision_id,core_artifact_id,
  adapter_abi,save_abi,dependency_snapshot_sha256,dat_version_id,dos_entry_path,payload_blob_id,payload_kind,
  native_profile,resume_slot,payload_sha256,payload_size_bytes,source_launch_session_id,created_at_ms
ON save_states
BEGIN SELECT RAISE(ABORT,'save checkpoint binding is immutable'); END;

CREATE TRIGGER isolated_runtime_bootstrap_tickets_insert
BEFORE INSERT ON isolated_runtime_bootstrap_tickets
WHEN NOT (
  NEW.launch_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM launch_sessions launch
    JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
    WHERE launch.id=NEW.launch_id AND launch.profile_id=NEW.profile_id
      AND launch.state='CREATED'
      AND artifact.runtime_adapter_kind IN ('NATIVE_WEB','TYRANOSCRIPT_WEB')
      AND NEW.expires_at_ms=launch.created_at_ms+60000
      AND NEW.expires_at_ms<=launch.bootstrap_expires_at_ms
  )
  OR NEW.preview_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM review_preview_sessions preview
    JOIN users actor ON actor.id=preview.actor_user_id
    JOIN core_artifacts artifact ON artifact.id=preview.core_artifact_id
    WHERE preview.id=NEW.preview_id AND actor.profile_id=NEW.profile_id
      AND preview.state='CREATED' AND artifact.runtime_family='TYRANOSCRIPT'
      AND artifact.runtime_adapter_kind='TYRANOSCRIPT_WEB'
      AND NEW.expires_at_ms=preview.created_at_ms+60000
      AND NEW.expires_at_ms<=preview.bootstrap_expires_at_ms
  )
)
BEGIN SELECT RAISE(ABORT,'invalid isolated runtime bootstrap ticket'); END;

CREATE TRIGGER isolated_runtime_bootstrap_tickets_consume
BEFORE UPDATE ON isolated_runtime_bootstrap_tickets
WHEN OLD.consumed_at_ms IS NOT NULL
  OR NEW.ticket_sha256<>OLD.ticket_sha256 OR NEW.launch_id IS NOT OLD.launch_id
  OR NEW.preview_id IS NOT OLD.preview_id
  OR NEW.profile_id<>OLD.profile_id OR NEW.expected_origin<>OLD.expected_origin
  OR NEW.expires_at_ms<>OLD.expires_at_ms OR NEW.consumed_at_ms IS NULL
  OR NOT (
    OLD.launch_id IS NOT NULL AND EXISTS(SELECT 1 FROM launch_sessions launch
      WHERE launch.id=OLD.launch_id AND launch.state IN ('CREATED','ACTIVE')
        AND NEW.consumed_at_ms<=OLD.expires_at_ms)
    OR OLD.preview_id IS NOT NULL AND EXISTS(SELECT 1 FROM review_preview_sessions preview
      WHERE preview.id=OLD.preview_id AND preview.state IN ('CREATED','ACTIVE')
        AND NEW.consumed_at_ms<=OLD.expires_at_ms)
  )
BEGIN SELECT RAISE(ABORT,'invalid bootstrap ticket consumption'); END;

CREATE TRIGGER isolated_runtime_bootstrap_tickets_immutable_delete
BEFORE DELETE ON isolated_runtime_bootstrap_tickets
WHEN OLD.launch_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM review_preview_sessions preview
  WHERE preview.id=OLD.preview_id AND preview.state NOT IN ('EXPIRED','REVOKED')
)
BEGIN SELECT RAISE(ABORT,'bootstrap ticket is retained for audit'); END;

CREATE TRIGGER isolated_runtime_capabilities_insert
BEFORE INSERT ON isolated_runtime_capabilities
WHEN NOT (
  NEW.launch_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM launch_sessions launch
    JOIN isolated_runtime_bootstrap_tickets ticket ON ticket.launch_id=launch.id
    WHERE launch.id=NEW.launch_id AND launch.profile_id=NEW.profile_id
      AND launch.state IN ('CREATED','ACTIVE')
      AND ticket.profile_id=NEW.profile_id AND ticket.expected_origin=NEW.expected_origin
      AND ticket.consumed_at_ms IS NOT NULL AND NEW.issued_at_ms=ticket.consumed_at_ms
      AND NEW.issued_at_ms<=ticket.expires_at_ms AND NEW.expires_at_ms<=launch.hard_expires_at_ms
      AND NEW.revoked_at_ms IS NULL
  )
  OR NEW.preview_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM review_preview_sessions preview
    JOIN users actor ON actor.id=preview.actor_user_id
    JOIN isolated_runtime_bootstrap_tickets ticket ON ticket.preview_id=preview.id
    WHERE preview.id=NEW.preview_id AND actor.profile_id=NEW.profile_id
      AND preview.state IN ('CREATED','ACTIVE')
      AND ticket.profile_id=NEW.profile_id AND ticket.expected_origin=NEW.expected_origin
      AND ticket.consumed_at_ms IS NOT NULL AND NEW.issued_at_ms=ticket.consumed_at_ms
      AND NEW.issued_at_ms<=ticket.expires_at_ms AND NEW.expires_at_ms<=preview.hard_expires_at_ms
      AND NEW.revoked_at_ms IS NULL
  )
)
BEGIN SELECT RAISE(ABORT,'invalid isolated runtime capability'); END;

CREATE TRIGGER isolated_runtime_capabilities_revoke
BEFORE UPDATE ON isolated_runtime_capabilities
WHEN OLD.revoked_at_ms IS NOT NULL OR NEW.credential_sha256<>OLD.credential_sha256
  OR NEW.launch_id IS NOT OLD.launch_id OR NEW.preview_id IS NOT OLD.preview_id
  OR NEW.profile_id<>OLD.profile_id
  OR NEW.expected_origin<>OLD.expected_origin OR NEW.issued_at_ms<>OLD.issued_at_ms
  OR NEW.expires_at_ms<>OLD.expires_at_ms OR NEW.revoked_at_ms IS NULL
BEGIN SELECT RAISE(ABORT,'invalid isolated runtime capability revocation'); END;

CREATE TRIGGER isolated_runtime_capabilities_immutable_delete
BEFORE DELETE ON isolated_runtime_capabilities
WHEN OLD.launch_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM review_preview_sessions preview
  WHERE preview.id=OLD.preview_id AND preview.state NOT IN ('EXPIRED','REVOKED')
)
BEGIN SELECT RAISE(ABORT,'isolated runtime capability is retained for audit'); END;

CREATE TRIGGER upload_sessions_purpose_immutable
BEFORE UPDATE OF purpose ON upload_sessions
BEGIN SELECT RAISE(ABORT,'upload purpose is immutable'); END;

CREATE TRIGGER upload_consumptions_rpgmaker_purpose_insert
BEFORE INSERT ON upload_consumptions
WHEN NOT EXISTS(
  SELECT 1 FROM upload_sessions upload
  WHERE upload.id=NEW.upload_session_id AND (
    upload.purpose='GENERAL' AND NEW.consumer_type<>'RUNTIME_ASSET_PACK_INSTALLATION'
    OR upload.purpose='RPG_MAKER_PROJECT'
      AND NEW.consumer_type IN ('IMPORT_JOB','GAME_FILE_REVISION_JOB')
    OR upload.purpose='ONS_PROJECT' AND NEW.consumer_type='IMPORT_JOB'
    OR upload.purpose='KIRIKIRI_PROJECT' AND NEW.consumer_type='IMPORT_JOB'
    OR upload.purpose='BUTTERSCOTCH_PROJECT' AND NEW.consumer_type='IMPORT_JOB'
    OR upload.purpose='TYRANOSCRIPT_PROJECT' AND NEW.consumer_type='IMPORT_JOB'
    OR upload.purpose='RUNTIME_ASSET_PACK'
      AND NEW.consumer_type='RUNTIME_ASSET_PACK_INSTALLATION'
      AND NEW.upload_file_id IS NULL
      AND EXISTS(SELECT 1 FROM runtime_asset_pack_installations installation
        WHERE installation.id=NEW.consumer_id)
  )
)
BEGIN SELECT RAISE(ABORT,'upload purpose/consumer mismatch'); END;
