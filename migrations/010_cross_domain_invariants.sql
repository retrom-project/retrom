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

CREATE UNIQUE INDEX core_artifacts_one_enabled_per_core ON core_artifacts(core_id) WHERE enabled = 1;

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

CREATE INDEX fk_platform_instances_default_core ON platform_instances(default_core_id);

CREATE INDEX fk_upload_files_session ON upload_files(upload_session_id);

CREATE INDEX fk_variant_revision_content ON game_variant_revisions(game_content_revision_id);

CREATE INDEX game_tags_tag ON game_tags(tag_id,game_id);

CREATE INDEX game_variants_game ON game_variants(game_id, core_id);

CREATE INDEX games_library ON games(status, platform_instance_id, search_text, id);

CREATE INDEX import_items_queue ON import_items(state, updated_at_ms, id);

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

CREATE INDEX review_runtime_screenshots_artifact ON review_runtime_screenshots(core_artifact_id);

CREATE INDEX review_runtime_screenshots_blob ON review_runtime_screenshots(blob_id);

CREATE INDEX review_runtime_screenshots_preview ON review_runtime_screenshots(preview_session_id);

CREATE INDEX review_runtime_screenshots_source ON review_runtime_screenshots(source_snapshot_id);

CREATE INDEX review_runtime_screenshots_validation ON review_runtime_screenshots(validation_id);

CREATE INDEX review_uploaded_assets_item ON review_uploaded_assets(import_item_id, created_at_ms, id);

CREATE INDEX save_states_library ON save_states(profile_id, game_id, created_at_ms DESC, id DESC);

CREATE INDEX save_states_source_launch
ON save_states(source_launch_session_id, created_at_ms DESC, id DESC)
WHERE deleted_at_ms IS NULL;

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
    SELECT 1 FROM game_variant_revisions WHERE id = NEW.current_revision_id AND game_variant_id = NEW.id AND status = 'READY'
  ) THEN RAISE(ABORT, 'variant current must be ready and owned') END;
END;

CREATE TRIGGER game_variants_current_ready_update
BEFORE UPDATE OF current_revision_id ON game_variants WHEN NEW.current_revision_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_variant_revisions WHERE id = NEW.current_revision_id AND game_variant_id = NEW.id AND status = 'READY'
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
  JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
  JOIN games game ON game.id=launch.game_id
  WHERE launch.id=OLD.launch_session_id AND (
    game.status='PUBLISHED' AND launch.state IN ('FINISHED','EXPIRED','REVOKED') AND (
      game.current_content_revision_id<>revision.game_content_revision_id OR EXISTS(
        SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
        JOIN bios_installations installation
          ON installation.id=json_extract(dependency.value,'$.installationId')
        WHERE installation.payload_released_at_ms IS NOT NULL
      )
    ) OR
    game.status='DELETED' AND game.payload_state IN ('RELEASING','FAILED')
  )
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER launch_content_files_immutable_update
BEFORE UPDATE ON launch_content_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER launch_content_files_published_insert BEFORE INSERT ON launch_content_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch JOIN games game ON game.id=launch.game_id
  WHERE launch.id=NEW.launch_session_id AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER launch_external_files_immutable_delete
BEFORE DELETE ON launch_external_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
  JOIN games game ON game.id=launch.game_id
  WHERE launch.id=OLD.launch_session_id AND (
    game.status='PUBLISHED' AND launch.state IN ('FINISHED','EXPIRED','REVOKED') AND (
      game.current_content_revision_id<>revision.game_content_revision_id OR EXISTS(
        SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
        JOIN bios_installations installation
          ON installation.id=json_extract(dependency.value,'$.installationId')
        WHERE installation.payload_released_at_ms IS NOT NULL
      )
    ) OR
    game.status='DELETED' AND game.payload_state IN ('RELEASING','FAILED')
  )
)
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_immutable_update
BEFORE UPDATE ON launch_external_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_published_insert BEFORE INSERT ON launch_external_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch JOIN games game ON game.id=launch.game_id
  WHERE launch.id=NEW.launch_session_id AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

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
screenshot_only_count,duplicate_count,attachment_active_count,not_ready_or_stale_count,
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
  )
)
BEGIN SELECT RAISE(ABORT,'invalid review preview snapshot'); END;

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
    AND launch.profile_id=NEW.profile_id
    AND launch.game_id=NEW.game_id
    AND launch.game_variant_revision_id=NEW.game_variant_revision_id
    AND launch.core_artifact_id=NEW.core_artifact_id
    AND launch.dos_entry_path IS NEW.dos_entry_path
    AND revision.dat_version_id IS NEW.dat_version_id
  ) THEN RAISE(ABORT, 'save state source launch mismatch') END;
END;

CREATE TRIGGER save_states_published_insert BEFORE INSERT ON save_states
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER save_states_source_launch_update
BEFORE UPDATE OF source_launch_session_id,profile_id,game_id,game_variant_revision_id,core_artifact_id,dat_version_id,dos_entry_path
ON save_states
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1
    FROM launch_sessions launch
    JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
    WHERE launch.id=NEW.source_launch_session_id
    AND launch.profile_id=NEW.profile_id
    AND launch.game_id=NEW.game_id
    AND launch.game_variant_revision_id=NEW.game_variant_revision_id
    AND launch.core_artifact_id=NEW.core_artifact_id
    AND launch.dos_entry_path IS NEW.dos_entry_path
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
