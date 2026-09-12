-- Pre-release bootstrap: create the current domain model directly.

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

CREATE UNIQUE INDEX dat_bios_sets_one_default ON dat_bios_sets(dat_version_id, machine_name) WHERE is_default = 1;

CREATE INDEX dat_rom_entries_crc32 ON dat_rom_entries(dat_version_id, crc32) WHERE crc32 IS NOT NULL;

CREATE INDEX dat_rom_entries_sha1 ON dat_rom_entries(dat_version_id, sha1) WHERE sha1 IS NOT NULL;

CREATE UNIQUE INDEX dat_versions_active
ON dat_versions(provider_id,target_id) WHERE is_active=1;

CREATE UNIQUE INDEX dat_versions_bytes
ON dat_versions(provider_id,target_id,sha256,parser_version);

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

CREATE INDEX fk_game_variant_runtime_packs_installation
ON game_variant_runtime_packs(installation_id,game_variant_id);

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

CREATE INDEX fk_runtime_asset_pack_files_blob ON runtime_asset_pack_files(blob_id);

CREATE INDEX fk_runtime_asset_pack_installations_bundle ON runtime_asset_pack_installations(bundle_blob_id);

CREATE INDEX fk_runtime_asset_pack_installations_definition
ON runtime_asset_pack_installations(definition_id,status,created_at_ms,id);

CREATE INDEX fk_upload_files_session ON upload_files(upload_session_id);

CREATE INDEX game_tags_tag ON game_tags(tag_id,game_id);

CREATE INDEX game_variants_game ON game_variants(game_id, core_id);

CREATE INDEX games_library ON games(status, platform_instance_id, search_text, id);

CREATE INDEX import_group_requests_actor ON import_group_requests(actor_user_id);

CREATE INDEX import_items_queue ON import_items(state, updated_at_ms, id);

CREATE INDEX import_job_file_resolutions_actor
ON import_job_file_resolutions(actor_user_id,created_at_ms);

CREATE UNIQUE INDEX isolated_runtime_capability_origin
ON isolated_runtime_capabilities(expected_origin);

CREATE UNIQUE INDEX isolated_runtime_ticket_origin
ON isolated_runtime_bootstrap_tickets(expected_origin);

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

CREATE UNIQUE INDEX review_multidisc_attachment_active
ON review_multidisc_attachments(import_item_id) WHERE state IN ('QUEUED','RUNNING');

CREATE INDEX review_multidisc_attachment_actor
ON review_multidisc_attachments(requested_by_user_id,created_at_ms,id);

CREATE INDEX review_multidisc_attachment_history
ON review_multidisc_attachments(import_item_id,created_at_ms,id);

CREATE INDEX review_preview_files_blob ON review_preview_files(blob_id);

CREATE INDEX review_preview_sessions_actor ON review_preview_sessions(actor_user_id);

CREATE INDEX review_preview_sessions_item
ON review_preview_sessions(import_item_id,created_at_ms DESC,id DESC);

CREATE INDEX review_preview_sessions_source ON review_preview_sessions(source_snapshot_id);

CREATE INDEX review_preview_sessions_target ON review_preview_sessions(target_platform_instance_id);

CREATE INDEX review_preview_sessions_validation ON review_preview_sessions(validation_id);

CREATE INDEX review_queue ON review_drafts(updated_at_ms, import_item_id);

CREATE INDEX review_runtime_screenshots_blob ON review_runtime_screenshots(blob_id);

CREATE INDEX review_runtime_screenshots_preview ON review_runtime_screenshots(preview_session_id);

CREATE INDEX review_runtime_screenshots_source ON review_runtime_screenshots(source_snapshot_id);

CREATE INDEX review_runtime_screenshots_validation ON review_runtime_screenshots(validation_id);

CREATE INDEX review_uploaded_assets_item ON review_uploaded_assets(import_item_id, created_at_ms, id);

CREATE INDEX save_states_library ON save_states(profile_id, game_id, created_at_ms DESC, id DESC);

CREATE INDEX save_states_payload ON save_states(payload_blob_id);

CREATE INDEX save_states_source_launch
ON save_states(source_launch_session_id,created_at_ms DESC,id DESC)
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
