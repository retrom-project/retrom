-- retrom:foreign-keys-off
-- One-way pre-release migration from revision chains and Target contract hashes
-- to current Game/File/Variant state and Provider-owned Target declarations.
PRAGMA defer_foreign_keys=ON;

CREATE TEMP TABLE __runtime_target_bindings_backup AS SELECT * FROM runtime_target_bindings;

DROP VIEW IF EXISTS "save_state_runtime_compatibility";
DROP TRIGGER IF EXISTS "account_links_no_delete";
DROP TRIGGER IF EXISTS "account_links_terminal_immutable";
DROP TRIGGER IF EXISTS "archive_entries_immutable_update";
DROP TRIGGER IF EXISTS "audit_events_immutable_delete";
DROP TRIGGER IF EXISTS "audit_events_immutable_update";
DROP TRIGGER IF EXISTS "bios_installations_payload_terminal";
DROP TRIGGER IF EXISTS "bios_installations_source_insert";
DROP TRIGGER IF EXISTS "bios_installations_source_update";
DROP TRIGGER IF EXISTS "bios_requirements_delivery_insert";
DROP TRIGGER IF EXISTS "bios_requirements_delivery_update";
DROP TRIGGER IF EXISTS "bios_requirements_runtime_target_insert";
DROP TRIGGER IF EXISTS "content_hash_evidence_immutable_delete";
DROP TRIGGER IF EXISTS "content_hash_evidence_immutable_update";
DROP TRIGGER IF EXISTS "content_identity_claims_immutable_delete";
DROP TRIGGER IF EXISTS "content_identity_claims_immutable_update";
DROP TRIGGER IF EXISTS "dat_versions_runtime_target_insert";
DROP TRIGGER IF EXISTS "dos_entries_immutable_delete";
DROP TRIGGER IF EXISTS "dos_entries_immutable_update";
DROP TRIGGER IF EXISTS "emulationstation_asset_delete";
DROP TRIGGER IF EXISTS "emulationstation_asset_insert";
DROP TRIGGER IF EXISTS "emulationstation_asset_snapshot_update";
DROP TRIGGER IF EXISTS "emulationstation_asset_state_update";
DROP TRIGGER IF EXISTS "emulationstation_collection_delete";
DROP TRIGGER IF EXISTS "emulationstation_collection_insert";
DROP TRIGGER IF EXISTS "emulationstation_collection_json_insert";
DROP TRIGGER IF EXISTS "emulationstation_collection_snapshot_update";
DROP TRIGGER IF EXISTS "emulationstation_collection_tag_json_update";
DROP TRIGGER IF EXISTS "emulationstation_collection_tags_immutable_update";
DROP TRIGGER IF EXISTS "emulationstation_collection_tags_validate_delete";
DROP TRIGGER IF EXISTS "emulationstation_collection_tags_validate_insert";
DROP TRIGGER IF EXISTS "emulationstation_file_delete";
DROP TRIGGER IF EXISTS "emulationstation_file_insert";
DROP TRIGGER IF EXISTS "emulationstation_file_snapshot_update";
DROP TRIGGER IF EXISTS "emulationstation_file_state_update";
DROP TRIGGER IF EXISTS "emulationstation_gamelist_delete";
DROP TRIGGER IF EXISTS "emulationstation_gamelist_insert";
DROP TRIGGER IF EXISTS "emulationstation_gamelist_json_insert";
DROP TRIGGER IF EXISTS "emulationstation_gamelists_immutable_update";
DROP TRIGGER IF EXISTS "emulationstation_import_collections_runtime_target_update";
DROP TRIGGER IF EXISTS "emulationstation_import_delete";
DROP TRIGGER IF EXISTS "emulationstation_import_identity_update";
DROP TRIGGER IF EXISTS "emulationstation_import_initial_insert";
DROP TRIGGER IF EXISTS "emulationstation_import_job_update";
DROP TRIGGER IF EXISTS "emulationstation_import_lifecycle_update";
DROP TRIGGER IF EXISTS "emulationstation_import_scan_complete_update";
DROP TRIGGER IF EXISTS "emulationstation_import_scan_job_insert";
DROP TRIGGER IF EXISTS "emulationstation_import_state_update";
DROP TRIGGER IF EXISTS "emulationstation_import_terminal_counts_update";
DROP TRIGGER IF EXISTS "emulationstation_import_version_update";
DROP TRIGGER IF EXISTS "emulationstation_item_delete";
DROP TRIGGER IF EXISTS "emulationstation_item_execution_update";
DROP TRIGGER IF EXISTS "emulationstation_item_insert";
DROP TRIGGER IF EXISTS "emulationstation_item_json_insert";
DROP TRIGGER IF EXISTS "emulationstation_item_json_update";
DROP TRIGGER IF EXISTS "emulationstation_item_library_review_insert";
DROP TRIGGER IF EXISTS "emulationstation_item_library_review_update";
DROP TRIGGER IF EXISTS "emulationstation_item_payload_update";
DROP TRIGGER IF EXISTS "emulationstation_item_published_update";
DROP TRIGGER IF EXISTS "emulationstation_item_review_discarded_update";
DROP TRIGGER IF EXISTS "emulationstation_item_review_pending_update";
DROP TRIGGER IF EXISTS "emulationstation_item_snapshot_update";
DROP TRIGGER IF EXISTS "emulationstation_item_state_update";
DROP TRIGGER IF EXISTS "emulationstation_item_version_update";
DROP TRIGGER IF EXISTS "favorite_folder_games_immutable_update";
DROP TRIGGER IF EXISTS "favorite_folders_guarded_update";
DROP TRIGGER IF EXISTS "favorite_games_immutable_update";
DROP TRIGGER IF EXISTS "game_assets_immutable_delete";
DROP TRIGGER IF EXISTS "game_assets_immutable_update";
DROP TRIGGER IF EXISTS "game_assets_published_insert";
DROP TRIGGER IF EXISTS "game_content_files_immutable_delete";
DROP TRIGGER IF EXISTS "game_content_files_immutable_update";
DROP TRIGGER IF EXISTS "game_content_files_published_insert";
DROP TRIGGER IF EXISTS "game_content_revisions_emulationstation_source_insert";
DROP TRIGGER IF EXISTS "game_content_revisions_immutable_delete";
DROP TRIGGER IF EXISTS "game_content_revisions_immutable_update";
DROP TRIGGER IF EXISTS "game_content_revisions_pegasus_source_insert";
DROP TRIGGER IF EXISTS "game_content_revisions_review_snapshot_insert";
DROP TRIGGER IF EXISTS "game_metadata_revisions_emulationstation_source_insert";
DROP TRIGGER IF EXISTS "game_metadata_revisions_immutable_delete";
DROP TRIGGER IF EXISTS "game_metadata_revisions_immutable_update";
DROP TRIGGER IF EXISTS "game_metadata_revisions_pegasus_source_insert";
DROP TRIGGER IF EXISTS "game_tags_immutable_update";
DROP TRIGGER IF EXISTS "game_tags_validate_insert";
DROP TRIGGER IF EXISTS "game_variant_revision_runtime_packs_immutable_delete";
DROP TRIGGER IF EXISTS "game_variant_revision_runtime_packs_immutable_update";
DROP TRIGGER IF EXISTS "game_variant_revision_runtime_packs_validate_insert";
DROP TRIGGER IF EXISTS "game_variant_revisions_immutable_delete";
DROP TRIGGER IF EXISTS "game_variant_revisions_immutable_update";
DROP TRIGGER IF EXISTS "game_variant_revisions_runtime_target_insert";
DROP TRIGGER IF EXISTS "games_current_metadata_owner_insert";
DROP TRIGGER IF EXISTS "games_current_owner_update";
DROP TRIGGER IF EXISTS "games_deleted_is_terminal";
DROP TRIGGER IF EXISTS "import_group_requests_immutable_delete";
DROP TRIGGER IF EXISTS "import_group_requests_immutable_update";
DROP TRIGGER IF EXISTS "import_item_core_validation_snapshot_insert";
DROP TRIGGER IF EXISTS "import_item_core_validations_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_core_validations_immutable_update";
DROP TRIGGER IF EXISTS "import_item_core_validations_runtime_target_insert";
DROP TRIGGER IF EXISTS "import_item_duplicate_matches_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_duplicate_matches_immutable_update";
DROP TRIGGER IF EXISTS "import_item_multidisc_entries_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_multidisc_entries_immutable_update";
DROP TRIGGER IF EXISTS "import_item_multidisc_entries_owner_insert";
DROP TRIGGER IF EXISTS "import_item_source_files_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_source_files_immutable_update";
DROP TRIGGER IF EXISTS "import_item_source_snapshot_files_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_source_snapshot_files_immutable_update";
DROP TRIGGER IF EXISTS "import_item_source_snapshots_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_source_snapshots_immutable_update";
DROP TRIGGER IF EXISTS "import_item_source_snapshots_revision_insert";
DROP TRIGGER IF EXISTS "import_item_validation_files_immutable_delete";
DROP TRIGGER IF EXISTS "import_item_validation_files_immutable_update";
DROP TRIGGER IF EXISTS "import_items_review_handoff_kind_immutable";
DROP TRIGGER IF EXISTS "import_job_file_resolutions_immutable_delete";
DROP TRIGGER IF EXISTS "import_job_file_resolutions_immutable_update";
DROP TRIGGER IF EXISTS "import_jobs_runtime_target_insert";
DROP TRIGGER IF EXISTS "instance_state_default_password_no_reenable";
DROP TRIGGER IF EXISTS "instance_state_no_reopen";
DROP TRIGGER IF EXISTS "isolated_runtime_bootstrap_tickets_consume";
DROP TRIGGER IF EXISTS "isolated_runtime_bootstrap_tickets_immutable_delete";
DROP TRIGGER IF EXISTS "isolated_runtime_capabilities_immutable_delete";
DROP TRIGGER IF EXISTS "isolated_runtime_capabilities_insert";
DROP TRIGGER IF EXISTS "isolated_runtime_capabilities_revoke";
DROP TRIGGER IF EXISTS "job_events_immutable_delete";
DROP TRIGGER IF EXISTS "job_events_immutable_update";
DROP TRIGGER IF EXISTS "job_input_snapshots_immutable_delete";
DROP TRIGGER IF EXISTS "job_input_snapshots_immutable_update";
DROP TRIGGER IF EXISTS "launch_content_files_immutable_delete";
DROP TRIGGER IF EXISTS "launch_content_files_immutable_update";
DROP TRIGGER IF EXISTS "launch_content_files_published_insert";
DROP TRIGGER IF EXISTS "launch_external_files_immutable_delete";
DROP TRIGGER IF EXISTS "launch_external_files_immutable_update";
DROP TRIGGER IF EXISTS "launch_external_files_kind_insert";
DROP TRIGGER IF EXISTS "launch_external_files_published_insert";
DROP TRIGGER IF EXISTS "launch_sessions_netplay_immutable";
DROP TRIGGER IF EXISTS "launch_sessions_revoke_isolated_runtime";
DROP TRIGGER IF EXISTS "launch_sessions_runtime_target_immutable";
DROP TRIGGER IF EXISTS "launch_sessions_runtime_target_insert";
DROP TRIGGER IF EXISTS "metadata_provider_cache_owner_insert";
DROP TRIGGER IF EXISTS "netplay_events_immutable_delete";
DROP TRIGGER IF EXISTS "netplay_events_immutable_update";
DROP TRIGGER IF EXISTS "netplay_room_members_validate_insert";
DROP TRIGGER IF EXISTS "netplay_room_members_validate_update";
DROP TRIGGER IF EXISTS "netplay_rooms_current_session_fk_insert";
DROP TRIGGER IF EXISTS "netplay_rooms_current_session_fk_update";
DROP TRIGGER IF EXISTS "netplay_rooms_current_session_immutable";
DROP TRIGGER IF EXISTS "netplay_rooms_host_immutable";
DROP TRIGGER IF EXISTS "netplay_rooms_require_host_after_update";
DROP TRIGGER IF EXISTS "netplay_rooms_snapshot_immutable";
DROP TRIGGER IF EXISTS "netplay_session_participants_immutable_identity";
DROP TRIGGER IF EXISTS "netplay_session_participants_validate_insert";
DROP TRIGGER IF EXISTS "netplay_sessions_runtime_target_immutable";
DROP TRIGGER IF EXISTS "netplay_sessions_runtime_target_insert";
DROP TRIGGER IF EXISTS "netplay_sessions_validate_insert";
DROP TRIGGER IF EXISTS "pegasus_asset_snapshot_update";
DROP TRIGGER IF EXISTS "pegasus_collection_tags_immutable_update";
DROP TRIGGER IF EXISTS "pegasus_collection_tags_validate_delete";
DROP TRIGGER IF EXISTS "pegasus_collection_tags_validate_insert";
DROP TRIGGER IF EXISTS "pegasus_file_snapshot_update";
DROP TRIGGER IF EXISTS "pegasus_import_collections_runtime_target_update";
DROP TRIGGER IF EXISTS "pegasus_import_job_update";
DROP TRIGGER IF EXISTS "pegasus_import_scan_job_insert";
DROP TRIGGER IF EXISTS "pegasus_item_library_review_cross_owner_insert";
DROP TRIGGER IF EXISTS "pegasus_item_library_review_cross_owner_update";
DROP TRIGGER IF EXISTS "pegasus_item_manifest_update";
DROP TRIGGER IF EXISTS "pegasus_item_published_update";
DROP TRIGGER IF EXISTS "pegasus_item_review_discarded_update";
DROP TRIGGER IF EXISTS "pegasus_item_review_pending_update";
DROP TRIGGER IF EXISTS "pegasus_item_snapshot_update";
DROP TRIGGER IF EXISTS "pegasus_metadata_files_immutable_update";
DROP TRIGGER IF EXISTS "platform_cores_in_use_disable";
DROP TRIGGER IF EXISTS "platform_instances_enabled_default_insert";
DROP TRIGGER IF EXISTS "platform_instances_enabled_default_update";
DROP TRIGGER IF EXISTS "play_session_events_immutable_delete";
DROP TRIGGER IF EXISTS "play_session_events_immutable_update";
DROP TRIGGER IF EXISTS "profiles_require_user_after_initialization";
DROP TRIGGER IF EXISTS "provider_responses_immutable_delete";
DROP TRIGGER IF EXISTS "provider_responses_immutable_update";
DROP TRIGGER IF EXISTS "review_arcade_parent_owner_insert";
DROP TRIGGER IF EXISTS "review_arcade_parent_transition_update";
DROP TRIGGER IF EXISTS "review_bulk_approval_items_frozen_update";
DROP TRIGGER IF EXISTS "review_bulk_approval_items_owner_insert";
DROP TRIGGER IF EXISTS "review_bulk_approval_items_published_update";
DROP TRIGGER IF EXISTS "review_bulk_approval_job_insert";
DROP TRIGGER IF EXISTS "review_bulk_approvals_frozen_update";
DROP TRIGGER IF EXISTS "review_draft_runtime_pack_selections_validate_delete";
DROP TRIGGER IF EXISTS "review_draft_runtime_pack_selections_validate_insert";
DROP TRIGGER IF EXISTS "review_draft_runtime_pack_selections_validate_update";
DROP TRIGGER IF EXISTS "review_draft_tags_immutable_update";
DROP TRIGGER IF EXISTS "review_draft_tags_validate_delete";
DROP TRIGGER IF EXISTS "review_draft_tags_validate_insert";
DROP TRIGGER IF EXISTS "review_drafts_final_source_snapshot_update";
DROP TRIGGER IF EXISTS "review_drafts_runtime_binding_revision_update";
DROP TRIGGER IF EXISTS "review_drafts_source_snapshot_insert";
DROP TRIGGER IF EXISTS "review_drafts_source_snapshot_update";
DROP TRIGGER IF EXISTS "review_drafts_uploaded_cover_insert";
DROP TRIGGER IF EXISTS "review_drafts_uploaded_cover_update";
DROP TRIGGER IF EXISTS "review_drafts_validation_snapshot_insert";
DROP TRIGGER IF EXISTS "review_drafts_validation_snapshot_update";
DROP TRIGGER IF EXISTS "review_events_immutable_delete";
DROP TRIGGER IF EXISTS "review_events_immutable_update";
DROP TRIGGER IF EXISTS "review_events_v2_payload_free_insert";
DROP TRIGGER IF EXISTS "review_multidisc_attachment_delete";
DROP TRIGGER IF EXISTS "review_multidisc_attachment_identity_update";
DROP TRIGGER IF EXISTS "review_multidisc_attachment_owner_insert";
DROP TRIGGER IF EXISTS "review_multidisc_attachment_result_update";
DROP TRIGGER IF EXISTS "review_multidisc_attachment_terminal_update";
DROP TRIGGER IF EXISTS "review_multidisc_attachment_transition_update";
DROP TRIGGER IF EXISTS "review_preview_files_immutable_delete";
DROP TRIGGER IF EXISTS "review_preview_files_immutable_update";
DROP TRIGGER IF EXISTS "review_preview_files_validate_insert";
DROP TRIGGER IF EXISTS "review_preview_sessions_revoke_isolated_runtime";
DROP TRIGGER IF EXISTS "review_preview_sessions_runtime_target_insert";
DROP TRIGGER IF EXISTS "review_runtime_screenshots_runtime_target_insert";
DROP TRIGGER IF EXISTS "review_uploaded_assets_immutable_delete";
DROP TRIGGER IF EXISTS "review_uploaded_assets_immutable_update";
DROP TRIGGER IF EXISTS "rpgmaker_content_profiles_immutable_delete";
DROP TRIGGER IF EXISTS "rpgmaker_content_profiles_immutable_update";
DROP TRIGGER IF EXISTS "rpgmaker_content_profiles_validate_insert";
DROP TRIGGER IF EXISTS "rpgmaker_review_profiles_runtime_target_insert";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validation_checkpoints_format_insert";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validation_checkpoints_guarded_delete";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validation_checkpoints_immutable_update";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validation_gate_events_immutable_delete";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validation_gate_events_immutable_update";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validation_gate_events_insert";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validations_gate_sequence_update";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validations_pass";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validations_runtime_target_insert";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validations_terminal_immutable";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validations_transition";
DROP TRIGGER IF EXISTS "rpgmaker_runtime_validations_transition_evidence";
DROP TRIGGER IF EXISTS "rpgmaker_variant_profiles_immutable_delete";
DROP TRIGGER IF EXISTS "rpgmaker_variant_profiles_immutable_update";
DROP TRIGGER IF EXISTS "runtime_asset_pack_definitions_immutable_delete";
DROP TRIGGER IF EXISTS "runtime_asset_pack_definitions_immutable_update";
DROP TRIGGER IF EXISTS "runtime_asset_pack_files_guarded_delete";
DROP TRIGGER IF EXISTS "runtime_asset_pack_files_immutable_update";
DROP TRIGGER IF EXISTS "runtime_asset_pack_files_insert";
DROP TRIGGER IF EXISTS "runtime_asset_pack_installations_blob_insert";
DROP TRIGGER IF EXISTS "runtime_asset_pack_installations_diagnostic_update";
DROP TRIGGER IF EXISTS "runtime_asset_pack_installations_identity_update";
DROP TRIGGER IF EXISTS "runtime_asset_pack_installations_immutable_delete";
DROP TRIGGER IF EXISTS "runtime_asset_pack_installations_state_update";
DROP TRIGGER IF EXISTS "runtime_asset_pack_installations_version_update";
DROP TRIGGER IF EXISTS "save_states_disc_insert";
DROP TRIGGER IF EXISTS "save_states_disc_update";
DROP TRIGGER IF EXISTS "save_states_published_insert";
DROP TRIGGER IF EXISTS "save_states_runtime_target_immutable";
DROP TRIGGER IF EXISTS "save_states_runtime_target_insert";
DROP TRIGGER IF EXISTS "save_states_source_launch_immutable";
DROP TRIGGER IF EXISTS "scrape_attempts_immutable_delete";
DROP TRIGGER IF EXISTS "scrape_attempts_immutable_update";
DROP TRIGGER IF EXISTS "scrape_candidate_assets_published_insert";
DROP TRIGGER IF EXISTS "scrape_candidate_assets_published_update";
DROP TRIGGER IF EXISTS "scrape_candidates_immutable_delete";
DROP TRIGGER IF EXISTS "scrape_candidates_immutable_update";
DROP TRIGGER IF EXISTS "server_bios_import_items_runtime_target_insert";
DROP TRIGGER IF EXISTS "server_bios_items_installation_update";
DROP TRIGGER IF EXISTS "server_import_job_insert";
DROP TRIGGER IF EXISTS "tags_guarded_update";
DROP TRIGGER IF EXISTS "tags_no_delete";
DROP TRIGGER IF EXISTS "upload_consumptions_rpgmaker_purpose_insert";
DROP TRIGGER IF EXISTS "upload_sessions_purpose_immutable";
DROP TRIGGER IF EXISTS "users_deleted_terminal";
DROP TRIGGER IF EXISTS "users_identity_immutable";
DROP TRIGGER IF EXISTS "users_last_enabled_admin";
DROP TRIGGER IF EXISTS "users_no_physical_delete";
DROP TRIGGER IF EXISTS "variant_dependencies_immutable_delete";
DROP TRIGGER IF EXISTS "variant_dependencies_immutable_update";
DROP TRIGGER IF EXISTS "variant_files_immutable_delete";
DROP TRIGGER IF EXISTS "variant_files_immutable_update";
DROP TRIGGER IF EXISTS "variant_files_published_insert";
DROP INDEX IF EXISTS "account_links_creator";
DROP INDEX IF EXISTS "account_links_kind_created";
DROP INDEX IF EXISTS "account_links_target";
DROP INDEX IF EXISTS "audit_events_actor";
DROP INDEX IF EXISTS "audit_events_resource";
DROP INDEX IF EXISTS "auth_sessions_expiry";
DROP INDEX IF EXISTS "auth_sessions_user_active";
DROP INDEX IF EXISTS "bios_installations_active";
DROP INDEX IF EXISTS "bios_installations_server_candidate";
DROP INDEX IF EXISTS "dat_bios_sets_one_default";
DROP INDEX IF EXISTS "dat_rom_entries_crc32";
DROP INDEX IF EXISTS "dat_rom_entries_sha1";
DROP INDEX IF EXISTS "dat_versions_active";
DROP INDEX IF EXISTS "dat_versions_bytes";
DROP INDEX IF EXISTS "emulationstation_collection_tags_tag";
DROP INDEX IF EXISTS "emulationstation_collections_mapping";
DROP INDEX IF EXISTS "emulationstation_collections_page";
DROP INDEX IF EXISTS "emulationstation_gamelists_page";
DROP INDEX IF EXISTS "emulationstation_imports_history";
DROP INDEX IF EXISTS "emulationstation_imports_one_active_execution";
DROP INDEX IF EXISTS "emulationstation_imports_state";
DROP INDEX IF EXISTS "emulationstation_items_collection";
DROP INDEX IF EXISTS "emulationstation_items_library_review";
DROP INDEX IF EXISTS "emulationstation_items_outcome";
DROP INDEX IF EXISTS "emulationstation_items_page";
DROP INDEX IF EXISTS "favorite_folder_games_folder";
DROP INDEX IF EXISTS "favorite_folder_games_game";
DROP INDEX IF EXISTS "favorite_folders_profile_created";
DROP INDEX IF EXISTS "favorite_games_game";
DROP INDEX IF EXISTS "favorite_games_profile_created";
DROP INDEX IF EXISTS "fk_archive_entries_materialized";
DROP INDEX IF EXISTS "fk_bios_installations_blob";
DROP INDEX IF EXISTS "fk_game_content_game";
DROP INDEX IF EXISTS "fk_game_metadata_game";
DROP INDEX IF EXISTS "fk_game_variant_runtime_packs_installation";
DROP INDEX IF EXISTS "fk_import_item_duplicate_matches_content_revision";
DROP INDEX IF EXISTS "fk_import_item_duplicate_matches_game";
DROP INDEX IF EXISTS "fk_import_item_multidisc_blob";
DROP INDEX IF EXISTS "fk_import_item_multidisc_upload";
DROP INDEX IF EXISTS "fk_import_items_job";
DROP INDEX IF EXISTS "fk_import_job_file_resolutions_replacement";
DROP INDEX IF EXISTS "fk_import_jobs_platform";
DROP INDEX IF EXISTS "fk_import_jobs_reconfigured_from";
DROP INDEX IF EXISTS "fk_launch_external_files_blob";
DROP INDEX IF EXISTS "fk_launch_game";
DROP INDEX IF EXISTS "fk_launch_validation";
DROP INDEX IF EXISTS "fk_platform_instances_default_core";
DROP INDEX IF EXISTS "fk_runtime_asset_pack_files_blob";
DROP INDEX IF EXISTS "fk_runtime_asset_pack_installations_bundle";
DROP INDEX IF EXISTS "fk_runtime_asset_pack_installations_definition";
DROP INDEX IF EXISTS "fk_upload_files_session";
DROP INDEX IF EXISTS "fk_variant_revision_content";
DROP INDEX IF EXISTS "game_tags_tag";
DROP INDEX IF EXISTS "game_variants_game";
DROP INDEX IF EXISTS "games_library";
DROP INDEX IF EXISTS "import_group_requests_actor";
DROP INDEX IF EXISTS "import_items_queue";
DROP INDEX IF EXISTS "import_job_file_resolutions_actor";
DROP INDEX IF EXISTS "isolated_runtime_capability_origin";
DROP INDEX IF EXISTS "isolated_runtime_ticket_origin";
DROP INDEX IF EXISTS "job_events_job";
DROP INDEX IF EXISTS "job_events_scope";
DROP INDEX IF EXISTS "jobs_claim";
DROP INDEX IF EXISTS "jobs_scope";
DROP INDEX IF EXISTS "launch_sessions_one_netplay_participant";
DROP INDEX IF EXISTS "netplay_events_room";
DROP INDEX IF EXISTS "netplay_events_session";
DROP INDEX IF EXISTS "netplay_room_members_active_seat";
DROP INDEX IF EXISTS "netplay_room_members_profile";
DROP INDEX IF EXISTS "netplay_rooms_expiry";
DROP INDEX IF EXISTS "netplay_rooms_one_active_host";
DROP INDEX IF EXISTS "netplay_session_participants_profile";
DROP INDEX IF EXISTS "netplay_sessions_one_active_room";
DROP INDEX IF EXISTS "netplay_sessions_state";
DROP INDEX IF EXISTS "pegasus_collection_tags_tag";
DROP INDEX IF EXISTS "pegasus_collections_mapping";
DROP INDEX IF EXISTS "pegasus_collections_page";
DROP INDEX IF EXISTS "pegasus_imports_history";
DROP INDEX IF EXISTS "pegasus_imports_one_active_execution";
DROP INDEX IF EXISTS "pegasus_imports_state";
DROP INDEX IF EXISTS "pegasus_items_collection";
DROP INDEX IF EXISTS "pegasus_items_library_review";
DROP INDEX IF EXISTS "pegasus_items_outcome";
DROP INDEX IF EXISTS "pegasus_items_page";
DROP INDEX IF EXISTS "pegasus_metadata_page";
DROP INDEX IF EXISTS "platform_instances_catalog_template_key_unique";
DROP INDEX IF EXISTS "review_arcade_parent_active";
DROP INDEX IF EXISTS "review_arcade_parent_history";
DROP INDEX IF EXISTS "review_bulk_approval_items_state";
DROP INDEX IF EXISTS "review_bulk_approvals_history";
DROP INDEX IF EXISTS "review_bulk_approvals_one_active";
DROP INDEX IF EXISTS "review_draft_tags_tag";
DROP INDEX IF EXISTS "review_events_actor";
DROP INDEX IF EXISTS "review_events_history";
DROP INDEX IF EXISTS "review_multidisc_attachment_active";
DROP INDEX IF EXISTS "review_multidisc_attachment_actor";
DROP INDEX IF EXISTS "review_multidisc_attachment_history";
DROP INDEX IF EXISTS "review_preview_files_blob";
DROP INDEX IF EXISTS "review_preview_sessions_actor";
DROP INDEX IF EXISTS "review_preview_sessions_item";
DROP INDEX IF EXISTS "review_preview_sessions_source";
DROP INDEX IF EXISTS "review_preview_sessions_target";
DROP INDEX IF EXISTS "review_preview_sessions_validation";
DROP INDEX IF EXISTS "review_queue";
DROP INDEX IF EXISTS "review_runtime_screenshots_blob";
DROP INDEX IF EXISTS "review_runtime_screenshots_preview";
DROP INDEX IF EXISTS "review_runtime_screenshots_source";
DROP INDEX IF EXISTS "review_runtime_screenshots_validation";
DROP INDEX IF EXISTS "review_uploaded_assets_item";
DROP INDEX IF EXISTS "rpgmaker_runtime_validations_passed_binding";
DROP INDEX IF EXISTS "rpgmaker_validation_gate_launch";
DROP INDEX IF EXISTS "rpgmaker_validation_gate_terminal";
DROP INDEX IF EXISTS "runtime_targets_game_compatibility";
DROP INDEX IF EXISTS "runtime_targets_netplay_compatibility";
DROP INDEX IF EXISTS "save_states_library";
DROP INDEX IF EXISTS "save_states_payload";
DROP INDEX IF EXISTS "save_states_source_launch";
DROP INDEX IF EXISTS "server_bios_candidates_page";
DROP INDEX IF EXISTS "server_bios_candidates_selected";
DROP INDEX IF EXISTS "server_bios_items_page";
DROP INDEX IF EXISTS "server_imports_history";
DROP INDEX IF EXISTS "server_imports_one_active_kind";
DROP INDEX IF EXISTS "server_imports_state";
DROP INDEX IF EXISTS "tags_active_name_key";
DROP INDEX IF EXISTS "tags_active_page";
DROP INDEX IF EXISTS "tags_updated_page";
DROP INDEX IF EXISTS "upload_consumptions_whole_session";
DROP INDEX IF EXISTS "users_list_created";
DROP INDEX IF EXISTS "users_list_last_login";
DROP INDEX IF EXISTS "users_list_username";

CREATE TABLE "__new_runtime_targets" (
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id) ON DELETE CASCADE,
  target_id TEXT NOT NULL CHECK(
    length(target_id) BETWEEN 1 AND 64 AND target_id=lower(target_id)
    AND target_id NOT GLOB '*[^a-z0-9-]*'
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 120),
  target_options_schema_json TEXT NOT NULL CHECK(
    json_valid(target_options_schema_json) AND json_type(target_options_schema_json)='object'
  ),
  capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
  checkpoint_json TEXT CHECK(checkpoint_json IS NULL OR json_valid(checkpoint_json)),
  manifest_fragment_json TEXT NOT NULL CHECK(json_valid(manifest_fragment_json)),
  PRIMARY KEY(provider_id,target_id)
);

CREATE TABLE "__new_bios_requirements" (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('STATIC','DAT_MACHINE')),
  dat_machine_name TEXT,
  logical_name TEXT NOT NULL,
  requirement_mode TEXT NOT NULL CHECK(requirement_mode IN ('REQUIRED','OPTIONAL','CONDITIONAL')),
  condition_code TEXT,
  activation_options_json TEXT,
  catalog_digest TEXT NOT NULL CHECK(length(catalog_digest) = 64),
  size_bytes INTEGER CHECK(size_bytes IS NULL OR size_bytes >= 0),
  md5 TEXT,
  sha1 TEXT,
  sha256 TEXT,
  source_url TEXT NOT NULL,
  source_version TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL, delivery_kind TEXT NOT NULL DEFAULT 'BIOS_BUNDLE'
CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE')), emulator_path TEXT,
  UNIQUE(provider_id,target_id,logical_name),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK((source_kind = 'STATIC' AND dat_machine_name IS NULL) OR (source_kind = 'DAT_MACHINE' AND dat_machine_name IS NOT NULL))
);

CREATE TABLE "__new_dat_versions" (
  id TEXT PRIMARY KEY,
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  builtin_relative_path TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  parser_version TEXT NOT NULL,
  parse_status TEXT NOT NULL CHECK(parse_status IN ('PENDING','PARSING','READY','FAILED','CANCELLED')),
  is_active INTEGER NOT NULL CHECK(is_active IN (0,1)),
  machine_count INTEGER,
  rom_entry_count INTEGER,
  disk_entry_count INTEGER,
  bios_set_count INTEGER,
  default_bios_set_count INTEGER,
  explicit_bios_machine_count INTEGER,
  base_dependency_target_count INTEGER,
  unresolved_relation_count INTEGER,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  parsed_at_ms INTEGER,
  activated_at_ms INTEGER,
  UNIQUE(id,provider_id,target_id),
  UNIQUE(provider_id,target_id,sha256,parser_version),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK((parse_status = 'READY') = (parsed_at_ms IS NOT NULL)),
  CHECK(is_active = 0 OR parse_status = 'READY')
);

CREATE TABLE "__new_server_bios_import_items" (
  server_import_id TEXT NOT NULL REFERENCES server_imports(id),
  requirement_id TEXT NOT NULL REFERENCES bios_requirements(id),
  requirement_version INTEGER NOT NULL CHECK(requirement_version>=1),
  core_id TEXT NOT NULL REFERENCES cores(id),
  core_name_snapshot TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('STATIC','DAT_MACHINE')),
  logical_name TEXT NOT NULL,
  requirement_mode TEXT NOT NULL CHECK(requirement_mode IN ('REQUIRED','OPTIONAL','CONDITIONAL')),
  condition_code TEXT,
  activation_options_json TEXT,
  delivery_kind TEXT NOT NULL CHECK(delivery_kind IN ('BIOS_BUNDLE','EXTERNAL_FILE')),
  emulator_path TEXT,
  source_version TEXT NOT NULL,
  catalog_digest TEXT NOT NULL CHECK(length(catalog_digest)=64 AND catalog_digest=lower(catalog_digest)),
  dat_version_id TEXT REFERENCES dat_versions(id),
  dat_machine_name TEXT,
  expected_size_bytes INTEGER CHECK(expected_size_bytes IS NULL OR expected_size_bytes>=0),
  expected_md5 TEXT,
  expected_sha1 TEXT,
  expected_sha256 TEXT,
  active_installation_id_snapshot TEXT REFERENCES bios_installations(id),
  active_installation_version_snapshot INTEGER,
  active_blob_sha256_snapshot TEXT,
  active_status_snapshot TEXT,
  active_validated_requirement_version_snapshot INTEGER,
  state TEXT NOT NULL CHECK(state IN ('PENDING','EVALUATING','IMPORTED_MATCHED','IMPORTED_WARNING','IMPORTED_MISSING_ENTRY','NOT_FOUND','SKIPPED_EXISTING','SKIPPED_NOT_BETTER','ALREADY_SAME_BYTES','SOURCE_CHANGED','CATALOG_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  candidate_count INTEGER NOT NULL DEFAULT 0 CHECK(candidate_count>=0),
  match_method TEXT CHECK(match_method IS NULL OR match_method IN ('EXACT_HASH','EXPECTED_SIZE_FALLBACK','LARGEST_SIZE_FALLBACK','DAT_ENTRY_MATCH','DAT_ENTRY_WARNING','DAT_PARTIAL_FALLBACK')),
  selection_details_json TEXT,
  previous_installation_id TEXT REFERENCES bios_installations(id),
  new_installation_id TEXT REFERENCES bios_installations(id),
  outcome_code TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  PRIMARY KEY(server_import_id,requirement_id),
  CHECK((source_kind='STATIC' AND dat_version_id IS NULL AND dat_machine_name IS NULL) OR
        (source_kind='DAT_MACHINE' AND dat_version_id IS NOT NULL AND dat_machine_name IS NOT NULL)),
  CHECK((state IN ('PENDING','EVALUATING'))=(completed_at_ms IS NULL)),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id)
);

CREATE TABLE "__new_import_jobs" (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL UNIQUE REFERENCES upload_sessions(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  platform_instance_version INTEGER NOT NULL,
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  default_core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  dat_version_id TEXT REFERENCES dat_versions(id),
  metadata_provider TEXT NOT NULL CHECK(metadata_provider IN ('HASHEOUS','NONE')),
  config_snapshot_json TEXT NOT NULL,
  config_snapshot_digest TEXT NOT NULL CHECK(length(config_snapshot_digest) = 64),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','REVIEW_PENDING','PARTIAL_FAILURE','COMPLETED','CANCEL_REQUESTED','CANCELLED','FAILED')),
  total_item_count INTEGER NOT NULL DEFAULT 0,
  queued_item_count INTEGER NOT NULL DEFAULT 0,
  running_item_count INTEGER NOT NULL DEFAULT 0,
  review_pending_item_count INTEGER NOT NULL DEFAULT 0,
  published_item_count INTEGER NOT NULL DEFAULT 0,
  discarded_item_count INTEGER NOT NULL DEFAULT 0,
  failed_item_count INTEGER NOT NULL DEFAULT 0,
  cancelled_item_count INTEGER NOT NULL DEFAULT 0,
  ignored_file_count INTEGER NOT NULL DEFAULT 0,
  rejected_file_count INTEGER NOT NULL DEFAULT 0,
  last_error_code TEXT,
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED','FAILED')),
  payload_release_job_id TEXT UNIQUE REFERENCES jobs(id),
  payload_released_at_ms INTEGER,
  payload_last_error_code TEXT,
  cancel_requested_at_ms INTEGER,
  cancel_reason TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  completed_at_ms INTEGER, resolved_rejected_file_count INTEGER NOT NULL DEFAULT 0
CHECK(resolved_rejected_file_count BETWEEN 0 AND rejected_file_count), reconfigured_from_import_job_id TEXT REFERENCES import_jobs(id), already_imported_item_count INTEGER NOT NULL DEFAULT 0
CHECK(already_imported_item_count BETWEEN 0 AND discarded_item_count), already_imported_file_count INTEGER NOT NULL DEFAULT 0
CHECK(already_imported_file_count >= 0),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(total_item_count = queued_item_count + running_item_count + review_pending_item_count + published_item_count + discarded_item_count + failed_item_count + cancelled_item_count),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL AND payload_last_error_code IS NULL OR
    payload_state='FAILED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NOT NULL
  )
);

CREATE TABLE "__new_import_item_core_validations" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  platform_instance_version INTEGER NOT NULL CHECK(platform_instance_version>=1),
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  prepublish_generation INTEGER NOT NULL DEFAULT 4 CHECK(prepublish_generation IN (3,4)),
  dat_version_id TEXT REFERENCES dat_versions(id),
  default_dos_entry TEXT,
  source_manifest_digest TEXT NOT NULL,
  prepublish_input_digest TEXT NOT NULL CHECK(length(prepublish_input_digest)=64),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  UNIQUE(import_item_id,prepublish_input_digest),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id)
);

CREATE TABLE "__new_import_item_duplicate_matches" (
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  existing_game_id TEXT NOT NULL REFERENCES games(id),
  content_identity_digest TEXT NOT NULL
    CHECK(length(content_identity_digest) = 64 AND content_identity_digest = lower(content_identity_digest)),
  detected_stage TEXT NOT NULL CHECK(detected_stage = 'IDENTIFICATION'),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(import_item_id, existing_game_id)
);

CREATE TABLE "__new_review_drafts" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL UNIQUE REFERENCES import_items(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  selected_validation_id TEXT REFERENCES import_item_core_validations(id),
  selected_candidate_id TEXT REFERENCES scrape_candidates(id),
  cover_candidate_asset_id TEXT REFERENCES scrape_candidate_assets(id),
  background_candidate_asset_id TEXT REFERENCES scrape_candidate_assets(id),
  default_dos_entry TEXT,
  metadata_json TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL
, cover_uploaded_asset_id TEXT REFERENCES review_uploaded_assets(id), effective_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id));

CREATE TABLE "__new_rpgmaker_review_profiles" (
  review_draft_id TEXT PRIMARY KEY REFERENCES review_drafts(id),
  generation TEXT NOT NULL CHECK(generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  evidence_family TEXT NOT NULL CHECK(evidence_family IN ('RPG2K','RGSS','MV','MZ')),
  evidence_generation TEXT CHECK(evidence_generation IS NULL OR evidence_generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  evidence_confidence TEXT NOT NULL CHECK(evidence_confidence IN ('MATCHED','FAMILY_ONLY')),
  engine_version TEXT,
  entry_html_path TEXT,
  file_count INTEGER NOT NULL CHECK(file_count BETWEEN 1 AND 10000),
  total_bytes INTEGER NOT NULL CHECK(total_bytes BETWEEN 0 AND 34359738368),
  project_fingerprint TEXT NOT NULL CHECK(length(project_fingerprint)=64 AND project_fingerprint=lower(project_fingerprint)),
  requirements_sha256 TEXT NOT NULL CHECK(length(requirements_sha256)=64 AND requirements_sha256=lower(requirements_sha256)),
  analysis_json TEXT NOT NULL CHECK(json_valid(analysis_json) AND length(CAST(analysis_json AS BLOB))<=262144),
  self_contained_override INTEGER NOT NULL DEFAULT 0 CHECK(self_contained_override IN (0,1)),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  dependency_snapshot_sha256 TEXT NOT NULL CHECK(
    length(dependency_snapshot_sha256)=64 AND dependency_snapshot_sha256=lower(dependency_snapshot_sha256)
  ),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(
    evidence_confidence='FAMILY_ONLY' AND evidence_family='RPG2K' AND evidence_generation IS NULL
    OR evidence_confidence='MATCHED' AND evidence_generation IS NOT NULL
  ),
  CHECK(
    evidence_family='RPG2K' AND generation IN ('RPG2000','RPG2003')
    OR evidence_family='RGSS' AND generation IN ('RPGXP','RPGVX','RPGVXACE')
    OR evidence_family='MV' AND generation='RPGMV'
    OR evidence_family='MZ' AND generation='RPGMZ'
  ),
  CHECK(
    generation IN ('RPGMV','RPGMZ') AND entry_html_path='index.html'
    OR generation NOT IN ('RPGMV','RPGMZ') AND entry_html_path IS NULL
  )
);

CREATE TABLE "__new_rpgmaker_runtime_validations" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  review_version_at_create INTEGER NOT NULL CHECK(review_version_at_create>=1),
  effective_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  project_fingerprint TEXT NOT NULL CHECK(length(project_fingerprint)=64 AND project_fingerprint=lower(project_fingerprint)),
  generation TEXT NOT NULL CHECK(generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  evidence_generation TEXT CHECK(evidence_generation IS NULL OR evidence_generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  evidence_confidence TEXT NOT NULL CHECK(evidence_confidence IN ('MATCHED','FAMILY_ONLY')),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  dependency_snapshot_sha256 TEXT NOT NULL CHECK(
    length(dependency_snapshot_sha256)=64 AND dependency_snapshot_sha256=lower(dependency_snapshot_sha256)
  ),
  launch_id TEXT UNIQUE REFERENCES launch_sessions(id),
  restore_launch_id TEXT UNIQUE REFERENCES launch_sessions(id),
  state TEXT NOT NULL CHECK(state IN (
    'CREATED','STARTING','RUNNING','CHECKPOINTED','RESTORED','AWAITING_DECISION','PASSED','FAILED','EXPIRED'
  )),
  last_gate_sequence INTEGER NOT NULL DEFAULT 0 CHECK(last_gate_sequence>=0),
  machine_gates_json TEXT NOT NULL CHECK(json_valid(machine_gates_json) AND length(CAST(machine_gates_json AS BLOB))<=262144),
  evidence_screenshot_blob_id TEXT REFERENCES blobs(id),
  failure_code TEXT,
  decision_note TEXT CHECK(
    decision_note IS NULL OR length(decision_note)<=500 AND length(CAST(decision_note AS BLOB))<=2000
    AND instr(decision_note,char(0))=0
  ),
  decided_by_user_id TEXT REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  expires_at_ms INTEGER NOT NULL,
  decided_at_ms INTEGER,
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(expires_at_ms=created_at_ms+900000),
  CHECK((evidence_confidence='FAMILY_ONLY')=(evidence_generation IS NULL)),
  CHECK(evidence_generation IS NULL OR evidence_generation=generation),
  CHECK(launch_id IS NULL OR restore_launch_id IS NULL OR launch_id<>restore_launch_id),
  CHECK((decision_note IS NULL)=(decided_by_user_id IS NULL) AND (decided_by_user_id IS NULL)=(decided_at_ms IS NULL)),
  CHECK(decided_by_user_id IS NULL OR state IN ('PASSED','FAILED')),
  CHECK((state IN ('FAILED','EXPIRED'))=(failure_code IS NOT NULL)),
  CHECK(state<>'CREATED' OR launch_id IS NULL AND restore_launch_id IS NULL
    AND last_gate_sequence=0 AND evidence_screenshot_blob_id IS NULL),
  CHECK(state NOT IN ('STARTING','RUNNING','CHECKPOINTED','RESTORED','AWAITING_DECISION','PASSED')
    OR launch_id IS NOT NULL),
  CHECK(state NOT IN ('RESTORED','AWAITING_DECISION','PASSED') OR restore_launch_id IS NOT NULL),
  CHECK(state<>'PASSED' OR restore_launch_id IS NOT NULL AND evidence_screenshot_blob_id IS NOT NULL
    AND decided_by_user_id IS NOT NULL AND failure_code IS NULL),
  CHECK(state<>'FAILED' OR decided_by_user_id IS NULL OR decision_note IS NOT NULL AND length(decision_note)>0)
);

CREATE TABLE "__new_review_arcade_parent_attachments" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  review_draft_id TEXT NOT NULL REFERENCES review_drafts(id),
  base_source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  result_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  dependency_machine TEXT NOT NULL CHECK(length(CAST(dependency_machine AS BLOB)) BETWEEN 1 AND 255),
  expected_logical_name TEXT NOT NULL,
  required_by_machine TEXT NOT NULL CHECK(length(CAST(required_by_machine AS BLOB)) BETWEEN 1 AND 255),
  depth INTEGER NOT NULL CHECK(depth BETWEEN 1 AND 63),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  dat_version_id TEXT NOT NULL REFERENCES dat_versions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  accepted_blob_id TEXT REFERENCES blobs(id),
  payload_released_at_ms INTEGER,
  original_filename TEXT NOT NULL CHECK(length(CAST(original_filename AS BLOB)) BETWEEN 1 AND 255),
  observed_size_bytes INTEGER CHECK(observed_size_bytes IS NULL OR observed_size_bytes >= 0),
  observed_sha256 TEXT CHECK(observed_sha256 IS NULL OR (length(observed_sha256)=64 AND observed_sha256=lower(observed_sha256))),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED')),
  error_code TEXT,
  diagnostics_json TEXT NOT NULL CHECK(length(CAST(diagnostics_json AS BLOB)) <= 65536),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  finished_at_ms INTEGER,
  CHECK(expected_logical_name=dependency_machine||'.zip'),
  CHECK((state='ACCEPTED')=(result_source_snapshot_id IS NOT NULL)),
  CHECK(state<>'ACCEPTED' AND accepted_blob_id IS NULL AND payload_released_at_ms IS NULL OR
        state='ACCEPTED' AND accepted_blob_id IS NOT NULL AND payload_released_at_ms IS NULL OR
        state='ACCEPTED' AND accepted_blob_id IS NULL AND payload_released_at_ms IS NOT NULL),
  CHECK((state IN ('REJECTED','FAILED_RETRYABLE','CANCELLED'))=(error_code IS NOT NULL)),
  CHECK((state IN ('ACCEPTED','REJECTED','FAILED_RETRYABLE','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK(state IN ('QUEUED','RUNNING') OR upload_file_id IS NULL OR observed_size_bytes IS NOT NULL)
);

CREATE TABLE "__new_review_preview_sessions" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  target_platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  actor_user_id TEXT NOT NULL REFERENCES users(id),
  idempotency_key TEXT NOT NULL,
  title TEXT NOT NULL CHECK(length(CAST(title AS BLOB)) BETWEEN 1 AND 800),
  content_kind TEXT NOT NULL REFERENCES content_kinds(id),
  content_blob_id TEXT NOT NULL REFERENCES blobs(id),
  content_logical_name TEXT NOT NULL CHECK(length(CAST(content_logical_name AS BLOB)) BETWEEN 1 AND 512),
  content_format TEXT NOT NULL CHECK(
    length(content_format) BETWEEN 2 AND 64 AND content_format=upper(content_format)
    AND content_format NOT GLOB '*[^A-Z0-9_]*'
  ),
  dependency_snapshot_json TEXT NOT NULL,
  default_dos_entry TEXT,
  emulator_game_id INTEGER CHECK(emulator_game_id IS NULL OR emulator_game_id>0),
  capture_allowed INTEGER NOT NULL CHECK(capture_allowed IN (0,1)),
  credential_sha256 BLOB NOT NULL CHECK(length(credential_sha256)=32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms INTEGER NOT NULL CHECK(bootstrap_expires_at_ms>=0),
  hard_expires_at_ms INTEGER NOT NULL CHECK(hard_expires_at_ms>=bootstrap_expires_at_ms),
  activated_at_ms INTEGER,
  finished_at_ms INTEGER,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  UNIQUE(actor_user_id,idempotency_key),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(state!='ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('EXPIRED','REVOKED'))=(finished_at_ms IS NOT NULL))
);

CREATE TABLE "__new_review_runtime_screenshots" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT NOT NULL REFERENCES import_items(id),
  preview_session_id TEXT NOT NULL REFERENCES review_preview_sessions(id),
  source_snapshot_id TEXT NOT NULL REFERENCES import_item_source_snapshots(id),
  validation_id TEXT NOT NULL REFERENCES import_item_core_validations(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  media_type TEXT NOT NULL CHECK(media_type IN ('image/png','image/jpeg')),
  width_px INTEGER NOT NULL CHECK(width_px BETWEEN 1 AND 40000000),
  height_px INTEGER NOT NULL CHECK(height_px BETWEEN 1 AND 40000000),
  captured_after_ms INTEGER NOT NULL CHECK(captured_after_ms=5000),
  captured_at_ms INTEGER NOT NULL CHECK(captured_at_ms>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=0),
  UNIQUE(import_item_id,validation_id),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id)
);

CREATE TABLE "__new_metadata_scrape_runs" (
  id TEXT PRIMARY KEY,
  import_item_id TEXT REFERENCES import_items(id),
  game_id TEXT REFERENCES games(id),
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
  provider TEXT NOT NULL CHECK(provider IN ('HASHEOUS','NONE')),
  provider_config_version INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('RUNNING','COMPLETED','FAILED','CANCELLED')),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  completed_at_ms INTEGER,
  error_code TEXT,
  CHECK((import_item_id IS NOT NULL) != (game_id IS NOT NULL)),
  CHECK((state = 'RUNNING') = (completed_at_ms IS NULL)),
  CHECK((state = 'FAILED') = (error_code IS NOT NULL))
);

CREATE TABLE "__new_jobs" (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN (
    'UPLOAD_FINALIZE','IMPORT_GROUP','IMPORT_ITEM_PIPELINE','DAT_PARSE','VARIANT_VALIDATE',
    'METADATA_SCRAPE','MEDIA_FETCH','GAME_CONTENT_REPLACE','BLOB_GC','UPLOAD_CLEANUP',
    'REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE','SERVER_BIOS_IMPORT',
    'SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_SCAN',
      'SERVER_EMULATIONSTATION_IMPORT','REVIEW_BULK_APPROVE','PAYLOAD_RELEASE',
      'RUNTIME_ASSET_PACK_VALIDATE'
  )),
  dedupe_key TEXT NOT NULL CHECK(length(dedupe_key)=64),
  execution_no INTEGER NOT NULL CHECK(execution_no>=1),
  payload_json TEXT NOT NULL,
  cancellable INTEGER NOT NULL CHECK(cancellable IN (0,1)),
  state TEXT NOT NULL CHECK(state IN ('QUEUED','RUNNING','CANCEL_REQUESTED','SUCCEEDED','FAILED','CANCELLED')),
  attempt_count INTEGER NOT NULL CHECK(attempt_count>=0),
  max_attempts INTEGER NOT NULL CHECK(max_attempts BETWEEN 1 AND 4),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  available_at_ms INTEGER NOT NULL CHECK(available_at_ms>=0),
  execution_started_at_ms INTEGER,
  execution_deadline_at_ms INTEGER,
  leased_until_ms INTEGER,
  heartbeat_at_ms INTEGER,
  finished_at_ms INTEGER,
  worker_id TEXT,
  error_code TEXT,
  error_retryable INTEGER CHECK(error_retryable IS NULL OR error_retryable IN (0,1)),
  cancel_requested_at_ms INTEGER,
  cancel_reason TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(kind,dedupe_key),
  CHECK((state IN ('SUCCEEDED','FAILED','CANCELLED'))=(finished_at_ms IS NOT NULL)),
  CHECK((state IN ('CANCEL_REQUESTED','CANCELLED'))=(cancel_requested_at_ms IS NOT NULL)),
  CHECK(kind NOT IN ('REVIEW_ARCADE_PARENT_VALIDATE','REVIEW_MULTI_DISC_VALIDATE') OR scope_type='IMPORT_ITEM'),
  CHECK(kind<>'SERVER_BIOS_IMPORT' OR scope_type='SERVER_IMPORT'),
  CHECK(kind NOT IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT') OR scope_type='PEGASUS_IMPORT'),
  CHECK((scope_type='EMULATIONSTATION_IMPORT')=(kind IN ('SERVER_EMULATIONSTATION_SCAN','SERVER_EMULATIONSTATION_IMPORT'))),
  CHECK(kind<>'REVIEW_BULK_APPROVE' OR scope_type='REVIEW_BULK_APPROVAL')
  ,CHECK(kind<>'RUNTIME_ASSET_PACK_VALIDATE' OR scope_type='RUNTIME_ASSET_PACK_INSTALLATION')
  ,CHECK(kind<>'PAYLOAD_RELEASE' OR scope_type IN (
    'IMPORT_ITEM','IMPORT_JOB','PEGASUS_IMPORT_ITEM','EMULATIONSTATION_IMPORT_ITEM','UPLOAD_CONSUMPTION','GAME'
  ))
  ,CHECK(scope_type<>'EMULATIONSTATION_IMPORT_ITEM' OR kind='PAYLOAD_RELEASE')
  ,CHECK(kind<>'PAYLOAD_RELEASE' OR (cancellable=0 AND max_attempts=4))
  ,CHECK(kind<>'BLOB_GC' OR (scope_type='BLOB' AND cancellable=0 AND max_attempts=4))
);

CREATE TABLE "__new_upload_consumptions" (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  consumer_type TEXT NOT NULL CHECK(consumer_type IN (
    'IMPORT_JOB','GAME_CONTENT_REPLACE_JOB','GAME_ASSET','REVIEW_ASSET','REVIEW_ARCADE_PARENT',
    'REVIEW_MULTI_DISC','BIOS_INSTALLATION','RUNTIME_ASSET_PACK_INSTALLATION'
  )),
  consumer_id TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  released_at_ms INTEGER,
  release_reason TEXT CHECK(release_reason IS NULL OR release_reason IN (
    'IMPORT_PUBLISHED','IMPORT_DISCARDED','IMPORT_FAILED_FINAL','IMPORT_CANCELLED',
    'IMPORT_JOB_TERMINAL','PEGASUS_TERMINAL','UPLOAD_CONSUMED','GAME_DELETED'
  )),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(consumer_type,consumer_id),
  CHECK((released_at_ms IS NULL)=(release_reason IS NULL))
);

CREATE TABLE "__new_pegasus_import_collections" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  metadata_relative_path TEXT NOT NULL CHECK(length(CAST(metadata_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  segment_ordinal INTEGER NOT NULL CHECK(segment_ordinal>=0),
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
  shortname TEXT,
  description TEXT NOT NULL DEFAULT '',
  game_count INTEGER NOT NULL CHECK(game_count>=0),
  issue_count INTEGER NOT NULL DEFAULT 0 CHECK(issue_count>=0),
  ignored_rules_json TEXT NOT NULL DEFAULT '[]',
  warning_fields_json TEXT NOT NULL DEFAULT '[]',
  mapping_action TEXT CHECK(mapping_action IS NULL OR mapping_action IN ('IMPORT','SKIP')),
  target_platform_instance_id TEXT REFERENCES platform_instances(id),
  target_platform_instance_version INTEGER CHECK(target_platform_instance_version IS NULL OR target_platform_instance_version>=1),
  target_platform_id TEXT REFERENCES platforms(id),
  target_default_core_id TEXT REFERENCES cores(id),
  target_provider_id TEXT REFERENCES runtime_providers(provider_id),
  target_id TEXT,

  target_dat_version_id TEXT REFERENCES dat_versions(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms), tag_snapshot_json TEXT NOT NULL DEFAULT '[]'
CHECK(json_valid(tag_snapshot_json) AND json_type(tag_snapshot_json)='array'),
  UNIQUE(import_id,metadata_relative_path,segment_ordinal),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_instance_version IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_platform_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_default_core_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_provider_id IS NOT NULL)),
  CHECK((mapping_action='IMPORT')=(target_id IS NOT NULL)),
  FOREIGN KEY(target_provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id)
);

CREATE TABLE "__new_pegasus_import_items" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES pegasus_imports(id),
  collection_id TEXT REFERENCES pegasus_import_collections(id),
  metadata_relative_path TEXT NOT NULL CHECK(length(CAST(metadata_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  game_ordinal INTEGER NOT NULL CHECK(game_ordinal>=0),
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key=lower(source_key)),
  title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  discovery_state TEXT NOT NULL CHECK(discovery_state IN ('READY','BLOCKED_SOURCE','BLOCKED_CONTENT')),
  execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  content_kind TEXT REFERENCES content_kinds(id),
  metadata_json TEXT NOT NULL,
  warnings_json TEXT NOT NULL DEFAULT '[]',
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  discovery_code TEXT,
  error_code TEXT,
  retryable INTEGER NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED','FAILED')),
  payload_release_job_id TEXT REFERENCES jobs(id),
  payload_released_at_ms INTEGER,
  payload_last_error_code TEXT,
  library_import_job_id TEXT REFERENCES import_jobs(id),
  library_import_item_id TEXT REFERENCES import_items(id),
  published_game_id TEXT REFERENCES games(id),
  existing_game_id TEXT REFERENCES games(id),
  existing_matches_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(existing_matches_json) AND json_type(existing_matches_json)='array'),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  error_details_json TEXT CHECK(
    error_details_json IS NULL OR (
      json_valid(error_details_json) AND json_type(error_details_json)='object'
      AND length(CAST(error_details_json AS BLOB))<=8192
    )
  ),
  UNIQUE(import_id,source_key),
  UNIQUE(import_id,metadata_relative_path,game_ordinal),
  CHECK((execution_state IN ('PENDING','COPYING','VALIDATING'))=(completed_at_ms IS NULL)),
  CHECK((execution_state='PUBLISHED')=(published_game_id IS NOT NULL)),
  CHECK((execution_state='SKIPPED_EXISTING')=(existing_game_id IS NOT NULL)),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL AND payload_last_error_code IS NULL OR
    payload_state='FAILED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NOT NULL
  )
);

CREATE TABLE "__new_emulationstation_import_collections" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES emulationstation_imports(id),
  gamelist_relative_path TEXT NOT NULL CHECK(length(CAST(gamelist_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  relative_directory TEXT NOT NULL CHECK(length(CAST(relative_directory AS BLOB))<=4096),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 200),
  game_count INTEGER NOT NULL CHECK(game_count>=0),
  issue_count INTEGER NOT NULL DEFAULT 0 CHECK(issue_count>=0),
  folder_entry_count INTEGER NOT NULL DEFAULT 0 CHECK(folder_entry_count>=0),
  hidden_game_count INTEGER NOT NULL DEFAULT 0 CHECK(hidden_game_count>=0),
  adult_game_count INTEGER NOT NULL DEFAULT 0 CHECK(adult_game_count>=0),
  extension_summary_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(extension_summary_json) AND json_type(extension_summary_json)='array' AND length(CAST(extension_summary_json AS BLOB))<=4096),
  extension_other_count INTEGER NOT NULL DEFAULT 0 CHECK(extension_other_count>=0),
  mapping_action TEXT CHECK(mapping_action IS NULL OR mapping_action IN ('IMPORT','SKIP')),
  target_platform_instance_id TEXT REFERENCES platform_instances(id),
  target_platform_instance_version INTEGER CHECK(target_platform_instance_version IS NULL OR target_platform_instance_version>=1),
  target_platform_id TEXT REFERENCES platforms(id),
  target_default_core_id TEXT REFERENCES cores(id),
  target_provider_id TEXT REFERENCES runtime_providers(provider_id),
  target_id TEXT,

  target_dat_version_id TEXT REFERENCES dat_versions(id),
  tag_snapshot_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(tag_snapshot_json) AND json_type(tag_snapshot_json)='array'),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(import_id,gamelist_relative_path),
  CHECK(game_count>0 OR mapping_action IS NULL OR mapping_action='SKIP'),
  CHECK(
    mapping_action IS NULL AND target_platform_instance_id IS NULL AND target_platform_instance_version IS NULL
      AND target_platform_id IS NULL AND target_default_core_id IS NULL AND target_provider_id IS NULL
      AND target_id IS NULL AND target_dat_version_id IS NULL AND tag_snapshot_json='[]'
    OR mapping_action='SKIP' AND target_platform_instance_id IS NULL AND target_platform_instance_version IS NULL
      AND target_platform_id IS NULL AND target_default_core_id IS NULL AND target_provider_id IS NULL
      AND target_id IS NULL AND target_dat_version_id IS NULL AND tag_snapshot_json='[]'
    OR mapping_action='IMPORT' AND target_platform_instance_id IS NOT NULL AND target_platform_instance_version IS NOT NULL
      AND target_platform_id IS NOT NULL AND target_default_core_id IS NOT NULL AND target_provider_id IS NOT NULL
      AND target_id IS NOT NULL
  ),
  FOREIGN KEY(target_provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id)
);

CREATE TABLE "__new_emulationstation_import_items" (
  id TEXT PRIMARY KEY,
  import_id TEXT NOT NULL REFERENCES emulationstation_imports(id),
  collection_id TEXT NOT NULL REFERENCES emulationstation_import_collections(id),
  gamelist_relative_path TEXT NOT NULL CHECK(length(CAST(gamelist_relative_path AS BLOB)) BETWEEN 1 AND 4096),
  game_ordinal INTEGER NOT NULL CHECK(game_ordinal>=1),
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key=lower(source_key)),
  title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  source_flags_json TEXT NOT NULL CHECK(json_valid(source_flags_json) AND json_type(source_flags_json)='object' AND length(CAST(source_flags_json AS BLOB))<=4096),
  discovery_state TEXT NOT NULL CHECK(discovery_state IN ('READY','BLOCKED_SOURCE','BLOCKED_CONTENT')),
  execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED')),
  content_kind TEXT NOT NULL REFERENCES content_kinds(id),
  metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json) AND json_type(metadata_json)='object' AND length(CAST(metadata_json AS BLOB))<=32768),
  warnings_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(warnings_json) AND json_type(warnings_json)='array' AND length(CAST(warnings_json AS BLOB))<=16384),
  source_manifest_json TEXT NOT NULL CHECK(json_valid(source_manifest_json) AND json_type(source_manifest_json)='object' AND length(CAST(source_manifest_json AS BLOB))<=32768),
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64 AND source_manifest_digest=lower(source_manifest_digest)),
  discovery_code TEXT,
  error_code TEXT,
  retryable INTEGER NOT NULL DEFAULT 0 CHECK(retryable IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED','FAILED')),
  payload_release_job_id TEXT REFERENCES jobs(id),
  payload_released_at_ms INTEGER,
  payload_last_error_code TEXT,
  library_import_job_id TEXT REFERENCES import_jobs(id),
  library_import_item_id TEXT REFERENCES import_items(id),
  published_game_id TEXT REFERENCES games(id),
  existing_game_id TEXT REFERENCES games(id),
  existing_matches_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(existing_matches_json) AND json_type(existing_matches_json)='array' AND length(CAST(existing_matches_json AS BLOB))<=32768),
  error_details_json TEXT CHECK(error_details_json IS NULL OR (json_valid(error_details_json) AND json_type(error_details_json)='object' AND length(CAST(error_details_json AS BLOB))<=8192)),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  completed_at_ms INTEGER,
  UNIQUE(import_id,source_key),
  UNIQUE(import_id,gamelist_relative_path,game_ordinal),
  CHECK((execution_state IN ('PENDING','COPYING','VALIDATING'))=(completed_at_ms IS NULL)),
  CHECK((execution_state='PUBLISHED')=(published_game_id IS NOT NULL)),
  CHECK((execution_state='SKIPPED_EXISTING')=(existing_game_id IS NOT NULL)),
  CHECK(payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
        payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
        payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL AND payload_last_error_code IS NULL OR
        payload_state='FAILED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NOT NULL)
);

CREATE TABLE "__new_launch_sessions" (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  purpose TEXT NOT NULL DEFAULT 'PRODUCT' CHECK(purpose IN ('PRODUCT','RPG_RUNTIME_VALIDATION')),
  game_id TEXT REFERENCES games(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  content_kind TEXT NOT NULL REFERENCES content_kinds(id),
  dependency_snapshot_json TEXT NOT NULL CHECK(json_valid(dependency_snapshot_json)),
  compatibility_code TEXT NOT NULL,
  effective_source_snapshot_id TEXT REFERENCES import_item_source_snapshots(id),
  rpgmaker_runtime_validation_id TEXT REFERENCES rpgmaker_runtime_validations(id),
  save_state_id TEXT REFERENCES save_states(id),
  dos_entry_path TEXT,
  return_to TEXT NOT NULL,
  credential_sha256 BLOB NOT NULL CHECK(length(credential_sha256) = 32),
  state TEXT NOT NULL CHECK(state IN ('CREATED','ACTIVE','FINISHED','EXPIRED','REVOKED')),
  bootstrap_expires_at_ms INTEGER NOT NULL,
  idle_expires_at_ms INTEGER,
  activated_at_ms INTEGER,
  finished_at_ms INTEGER,
  hard_expires_at_ms INTEGER NOT NULL,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  version INTEGER NOT NULL DEFAULT 1, initial_disc_index INTEGER NOT NULL DEFAULT 0 CHECK(initial_disc_index BETWEEN 0 AND 7), netplay_session_id TEXT REFERENCES netplay_sessions(id), netplay_player_no INTEGER CHECK(netplay_player_no IS NULL OR netplay_player_no BETWEEN 1 AND 4), save_access TEXT NOT NULL DEFAULT 'NORMAL'
  CHECK(save_access IN ('NORMAL','NETPLAY_DISABLED')),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(hard_expires_at_ms >= bootstrap_expires_at_ms),
  CHECK(state != 'ACTIVE' OR activated_at_ms IS NOT NULL),
  CHECK((state IN ('FINISHED','EXPIRED','REVOKED')) = (finished_at_ms IS NOT NULL)),
  CHECK(
    purpose='PRODUCT' AND game_id IS NOT NULL AND effective_source_snapshot_id IS NULL
      AND rpgmaker_runtime_validation_id IS NULL
    OR purpose='RPG_RUNTIME_VALIDATION' AND game_id IS NULL AND effective_source_snapshot_id IS NOT NULL
      AND rpgmaker_runtime_validation_id IS NOT NULL AND save_state_id IS NULL
      AND dos_entry_path IS NULL AND netplay_session_id IS NULL AND netplay_player_no IS NULL
      AND save_access='NORMAL' AND initial_disc_index=0
  )
);

CREATE TABLE "__new_launch_external_files" (
  launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  virtual_path TEXT NOT NULL CHECK(length(virtual_path) BETWEEN 1 AND 512),
  logical_name TEXT NOT NULL CHECK(length(logical_name) BETWEEN 1 AND 255),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0), kind TEXT NOT NULL DEFAULT 'BIOS' CHECK(kind IN ('BIOS','BIOS_BUNDLE','PARENT','DISC')),
  PRIMARY KEY(launch_session_id, virtual_path),
  UNIQUE(launch_session_id, logical_name),
  CHECK(substr(virtual_path,1,1)='/' AND
        virtual_path NOT LIKE '%\%' AND
        virtual_path NOT LIKE '%?%' AND
        virtual_path NOT LIKE '%#%' AND
        instr(virtual_path,char(0))=0 AND
        virtual_path NOT LIKE '%//%' AND
        virtual_path NOT LIKE '%/./%' AND
        virtual_path NOT LIKE '%/../%' AND
        virtual_path NOT LIKE '%/.' AND
        virtual_path NOT LIKE '%/..'),
  CHECK(logical_name NOT LIKE '%/%' AND
        logical_name NOT LIKE '%\%' AND
        logical_name NOT IN ('','.','..') AND
        instr(logical_name,char(0))=0)
);

CREATE TABLE "__new_save_states" (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  checkpoint_format TEXT NOT NULL CHECK(length(checkpoint_format) BETWEEN 1 AND 128),
  payload_blob_id TEXT NOT NULL REFERENCES blobs(id),
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256=lower(payload_sha256)),
  payload_size_bytes INTEGER NOT NULL CHECK(payload_size_bytes BETWEEN 1 AND 268435456),
  screenshot_blob_id TEXT REFERENCES blobs(id),
  name TEXT NOT NULL,
  active_duration_ms INTEGER NOT NULL CHECK(active_duration_ms >= 0),
  dos_entry_path TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  deleted_at_ms INTEGER,
  source_launch_session_id TEXT NOT NULL REFERENCES launch_sessions(id),
  disc_index INTEGER CHECK(disc_index BETWEEN 0 AND 7)
);

CREATE TABLE "__new_play_sessions" (
  id TEXT PRIMARY KEY,
  launch_session_id TEXT NOT NULL UNIQUE REFERENCES launch_sessions(id),
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  started_at_ms INTEGER NOT NULL,
  last_heartbeat_at_ms INTEGER NOT NULL,
  ended_at_ms INTEGER,
  active_duration_ms INTEGER NOT NULL DEFAULT 0 CHECK(active_duration_ms >= 0),
  last_client_sequence INTEGER NOT NULL DEFAULT 0 CHECK(last_client_sequence >= 0),
  state TEXT NOT NULL CHECK(state IN ('ACTIVE','FINISHED','ABANDONED')),
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  CHECK((state = 'ACTIVE') = (ended_at_ms IS NULL))
);

CREATE TABLE "__new_netplay_rooms" (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  host_profile_id TEXT NOT NULL REFERENCES profiles(id),
  state TEXT NOT NULL CHECK(state IN ('DRAFT','WAITING','STARTING','RUNNING','ENDED','EXPIRED')),
  selected_game_id TEXT REFERENCES games(id),
  selected_game_variant_id TEXT REFERENCES game_variants(id),
  netplay_profile_id TEXT,
  profile_digest TEXT CHECK(profile_digest IS NULL OR profile_digest GLOB '[0-9a-f]*' AND length(profile_digest)=64),
  max_players INTEGER CHECK(max_players IS NULL OR max_players BETWEEN 2 AND 4),
  current_session_id TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>=0),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  ended_at_ms INTEGER,
  end_reason TEXT CHECK(end_reason IS NULL OR end_reason IN (
    'NORMAL','USER_EXIT','HOST_CLOSED','HOST_LOST','PEER_TIMEOUT','AUTH_REVOKED','START_TIMEOUT',
    'PREPARE_FAILED','PROFILE_REVOKED','SERVER_RESTARTED','RESTORE','HARD_EXPIRED',
    'ROLLBACK_WINDOW_EXCEEDED','STATE_RING_CAPACITY_EXCEEDED','STATE_TRANSFER_TIMEOUT',
    'STATE_INVALID','NETPLAY_UNSTABLE','PEER_TOO_SLOW','PROTOCOL_VIOLATION','INTERNAL_ERROR','GAME_DELETED',
    'GAME_CONTENT_REPLACED','BIOS_REPLACED'
  )),
  CHECK(
    selected_game_id IS NULL AND selected_game_variant_id IS NULL AND netplay_profile_id IS NULL
      AND profile_digest IS NULL AND max_players IS NULL OR
    selected_game_id IS NOT NULL AND selected_game_variant_id IS NOT NULL AND netplay_profile_id IS NOT NULL
      AND profile_digest IS NOT NULL AND max_players IS NOT NULL
  ),
  CHECK(state!='DRAFT' OR selected_game_id IS NULL),
  CHECK(state NOT IN ('WAITING','STARTING','RUNNING') OR selected_game_id IS NOT NULL),
  CHECK((state IN ('STARTING','RUNNING'))=(current_session_id IS NOT NULL)),
  CHECK((state IN ('ENDED','EXPIRED'))=(ended_at_ms IS NOT NULL)),
  CHECK((ended_at_ms IS NULL)=(end_reason IS NULL)),
  CHECK(ended_at_ms IS NULL OR ended_at_ms>=created_at_ms)
);

CREATE TABLE "__new_netplay_sessions" (
  id TEXT PRIMARY KEY CHECK(id=lower(id)),
  room_id TEXT NOT NULL REFERENCES netplay_rooms(id),
  session_no INTEGER NOT NULL CHECK(session_no>=1),
  state TEXT NOT NULL CHECK(state IN (
    'PREPARING','LOADING','SYNCHRONIZING','RUNNING','PAUSED_RECONNECT','RESYNCHRONIZING','FINISHED','FAILED'
  )),
  game_id TEXT NOT NULL REFERENCES games(id),
  game_variant_id TEXT NOT NULL REFERENCES game_variants(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  netplay_profile_id TEXT NOT NULL,
  profile_json TEXT NOT NULL CHECK(json_valid(profile_json) AND json_type(profile_json)='object'),
  profile_digest TEXT NOT NULL CHECK(profile_digest GLOB '[0-9a-f]*' AND length(profile_digest)=64),
  player_count INTEGER NOT NULL CHECK(player_count BETWEEN 2 AND 4),
  occupied_seat_mask INTEGER NOT NULL CHECK(occupied_seat_mask BETWEEN 3 AND 15 AND (occupied_seat_mask & 1)=1),
  authority_player_no INTEGER NOT NULL DEFAULT 1 CHECK(authority_player_no=1),
  resync_count INTEGER NOT NULL DEFAULT 0 CHECK(resync_count>=0),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  started_at_ms INTEGER,
  finished_at_ms INTEGER,
  end_reason TEXT CHECK(end_reason IS NULL OR end_reason IN (
    'NORMAL','USER_EXIT','HOST_CLOSED','HOST_LOST','PEER_TIMEOUT','AUTH_REVOKED','START_TIMEOUT',
    'PREPARE_FAILED','PROFILE_REVOKED','SERVER_RESTARTED','RESTORE','HARD_EXPIRED',
    'ROLLBACK_WINDOW_EXCEEDED','STATE_RING_CAPACITY_EXCEEDED','STATE_TRANSFER_TIMEOUT',
    'STATE_INVALID','NETPLAY_UNSTABLE','PEER_TOO_SLOW','PROTOCOL_VIOLATION','INTERNAL_ERROR','GAME_DELETED',
    'GAME_CONTENT_REPLACED','BIOS_REPLACED'
  )),
  UNIQUE(room_id,session_no),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK((state IN ('FINISHED','FAILED'))=(finished_at_ms IS NOT NULL)),
  CHECK((finished_at_ms IS NULL)=(end_reason IS NULL)),
  CHECK(started_at_ms IS NULL OR started_at_ms>=created_at_ms),
  CHECK(finished_at_ms IS NULL OR finished_at_ms>=created_at_ms)
);

CREATE TABLE "__new_game_assets" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  kind TEXT NOT NULL CHECK(kind IN ('COVER','BACKGROUND','SCREENSHOT','VIDEO')),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  width_px INTEGER,
  height_px INTEGER,
  media_type TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(game_id,kind,ordinal),
  CHECK(
    (kind IN ('COVER','BACKGROUND','SCREENSHOT') AND width_px>0 AND height_px>0 AND media_type IN ('image/png','image/jpeg','image/webp')) OR
    (kind='VIDEO' AND ordinal=0 AND width_px IS NULL AND height_px IS NULL AND media_type IN ('video/mp4','video/webm'))
  )
);

CREATE TABLE "__new_game_variants" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id),
  target_id TEXT NOT NULL,
  dat_version_id TEXT REFERENCES dat_versions(id),
  emulator_game_id INTEGER UNIQUE CHECK(emulator_game_id IS NULL OR emulator_game_id BETWEEN 1 AND 9007199254740991),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  default_dos_entry TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(game_id,core_id),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id),
  CHECK(status='READY' OR emulator_game_id IS NULL)
);

CREATE TABLE "__new_games" (
  id TEXT PRIMARY KEY,
  platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  title TEXT NOT NULL CHECK(length(title)>0),
  title_initial TEXT NOT NULL CHECK(
    length(title_initial)=1 AND (title_initial='#' OR title_initial GLOB '[0-9]' OR title_initial GLOB '[A-Z]')
  ),
  description TEXT NOT NULL,
  developer TEXT NOT NULL,
  publisher TEXT NOT NULL,
  genre TEXT NOT NULL,
  players INTEGER CHECK(players IS NULL OR players BETWEEN 1 AND 64),
  release_year INTEGER,
  metadata_source_kind TEXT NOT NULL CHECK(metadata_source_kind IN (
    'IMPORT_REVIEW','ADMIN_EDIT','RESCRAPE_APPLY','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_IMPORT'
  )),
  metadata_source_ref_id TEXT,
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' REFERENCES content_kinds(id),
  content_source_kind TEXT NOT NULL CHECK(content_source_kind IN (
    'IMPORT_REVIEW','ADMIN_REPLACE','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_IMPORT'
  )),
  content_source_ref_id TEXT NOT NULL,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64),
  status TEXT NOT NULL CHECK(status IN ('PUBLISHED','DELETED')),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED','FAILED')),
  payload_release_job_id TEXT UNIQUE REFERENCES jobs(id),
  payload_released_at_ms INTEGER,
  payload_last_error_code TEXT,
  search_text TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  deleted_at_ms INTEGER,
  CHECK((status='DELETED')=(deleted_at_ms IS NOT NULL)),
  CHECK(status<>'PUBLISHED' OR payload_state='RETAINED'),
  CHECK(status<>'DELETED' OR payload_state IN ('RELEASING','RELEASED','FAILED')),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL AND payload_last_error_code IS NULL OR
    payload_state='FAILED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NOT NULL
  ),
  CHECK((metadata_source_kind='ADMIN_EDIT')=(metadata_source_ref_id IS NULL))
);

CREATE TABLE "__new_rpgmaker_variant_profiles" (
  game_variant_id TEXT PRIMARY KEY REFERENCES game_variants(id),
  generation TEXT NOT NULL CHECK(generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  dependency_snapshot_sha256 TEXT NOT NULL CHECK(
    length(dependency_snapshot_sha256)=64 AND dependency_snapshot_sha256=lower(dependency_snapshot_sha256)
  ),
  runtime_validation_id TEXT UNIQUE REFERENCES rpgmaker_runtime_validations(id)
);

CREATE TABLE "__new_variant_dependencies" (
  game_variant_id TEXT NOT NULL REFERENCES game_variants(id),
  kind TEXT NOT NULL CHECK(kind IN ('PARENT','BIOS_OR_BASE')),
  logical_archive TEXT NOT NULL,
  dat_version_id TEXT NOT NULL REFERENCES dat_versions(id),
  source_machine_name TEXT NOT NULL,
  required_entries_json TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('SATISFIED_BY_CONTENT','SATISFIED_EXTERNAL','HASH_WARNING','MISSING','MISMATCH','UNSUPPORTED')),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(game_variant_id,kind,logical_archive),
  CHECK(kind='BIOS_OR_BASE' OR state<>'HASH_WARNING')
);

CREATE TABLE "__new_variant_files" (
  game_variant_id TEXT NOT NULL REFERENCES game_variants(id),
  role TEXT NOT NULL CHECK(role IN (
    'PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_variant_id,role,logical_name)
);

CREATE TABLE "__new_dos_entries" (
  game_id TEXT NOT NULL REFERENCES games(id),
  normalized_path TEXT NOT NULL,
  original_relative_path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('EXE','COM','BAT')),
  rank INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  direct_launch_safe INTEGER NOT NULL CHECK(direct_launch_safe IN (0,1)),
  PRIMARY KEY(game_id, normalized_path)
);

CREATE TABLE "__new_game_files" (
  game_id TEXT NOT NULL REFERENCES games(id),
  role TEXT NOT NULL CHECK(role IN (
    'CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC','PROJECT_FILE',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
);

CREATE TABLE "__new_game_variant_runtime_packs" (
  game_variant_id TEXT NOT NULL REFERENCES game_variants(id),
  slot INTEGER NOT NULL CHECK(slot BETWEEN 0 AND 3),
  declared_name TEXT NOT NULL CHECK(length(CAST(declared_name AS BLOB)) BETWEEN 1 AND 512),
  normalized_declared_name TEXT NOT NULL CHECK(length(CAST(normalized_declared_name AS BLOB)) BETWEEN 1 AND 512),
  definition_id TEXT NOT NULL REFERENCES runtime_asset_pack_definitions(id),
  installation_id TEXT NOT NULL,
  PRIMARY KEY(game_variant_id,slot),
  FOREIGN KEY(installation_id,definition_id)
    REFERENCES runtime_asset_pack_installations(id,definition_id)
);

CREATE TABLE "__new_rpgmaker_game_profiles" (
  game_id TEXT PRIMARY KEY REFERENCES games(id),
  evidence_family TEXT NOT NULL CHECK(evidence_family IN ('RPG2K','RGSS','MV','MZ')),
  evidence_generation TEXT CHECK(evidence_generation IS NULL OR evidence_generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  evidence_confidence TEXT NOT NULL CHECK(evidence_confidence IN ('MATCHED','FAMILY_ONLY')),
  engine_version TEXT,
  entry_html_path TEXT,
  file_count INTEGER NOT NULL CHECK(file_count BETWEEN 1 AND 10000),
  total_bytes INTEGER NOT NULL CHECK(total_bytes BETWEEN 0 AND 34359738368),
  project_fingerprint TEXT NOT NULL CHECK(length(project_fingerprint)=64 AND project_fingerprint=lower(project_fingerprint)),
  requirements_sha256 TEXT NOT NULL CHECK(length(requirements_sha256)=64 AND requirements_sha256=lower(requirements_sha256)),
  analysis_json TEXT NOT NULL CHECK(json_valid(analysis_json) AND length(CAST(analysis_json AS BLOB))<=262144),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  CHECK(
    evidence_confidence='FAMILY_ONLY' AND evidence_family='RPG2K' AND evidence_generation IS NULL
    OR evidence_confidence='MATCHED' AND evidence_generation IS NOT NULL
  ),
  CHECK(
    evidence_family='RPG2K' AND (evidence_generation IS NULL OR evidence_generation IN ('RPG2000','RPG2003'))
    OR evidence_family='RGSS' AND evidence_generation IN ('RPGXP','RPGVX','RPGVXACE')
    OR evidence_family='MV' AND evidence_generation='RPGMV'
    OR evidence_family='MZ' AND evidence_generation='RPGMZ'
  ),
  CHECK(
    evidence_family IN ('MV','MZ') AND entry_html_path='index.html'
    OR evidence_family NOT IN ('MV','MZ') AND entry_html_path IS NULL
  )
);

INSERT INTO "__new_runtime_targets"("provider_id","target_id","display_name","target_options_schema_json","capabilities_json","checkpoint_json","manifest_fragment_json")
SELECT o."provider_id",
       o."target_id",
       o."display_name",
       o."target_options_schema_json",
       o."capabilities_json",
       o."checkpoint_json",
       o."manifest_fragment_json"
FROM "runtime_targets" o;

INSERT INTO "__new_bios_requirements"("id","core_id","provider_id","target_id","source_kind","dat_machine_name","logical_name","requirement_mode","condition_code","activation_options_json","catalog_digest","size_bytes","md5","sha1","sha256","source_url","source_version","enabled","version","created_at_ms","updated_at_ms","delivery_kind","emulator_path")
SELECT o."id",
       o."core_id",
       o."provider_id",
       o."target_id",
       o."source_kind",
       o."dat_machine_name",
       o."logical_name",
       o."requirement_mode",
       o."condition_code",
       o."activation_options_json",
       o."catalog_digest",
       o."size_bytes",
       o."md5",
       o."sha1",
       o."sha256",
       o."source_url",
       o."source_version",
       o."enabled",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."delivery_kind",
       o."emulator_path"
FROM "bios_requirements" o;

INSERT INTO "__new_dat_versions"("id","core_id","provider_id","target_id","builtin_relative_path","sha256","parser_version","parse_status","is_active","machine_count","rom_entry_count","disk_entry_count","bios_set_count","default_bios_set_count","explicit_bios_machine_count","base_dependency_target_count","unresolved_relation_count","version","created_at_ms","updated_at_ms","parsed_at_ms","activated_at_ms")
SELECT o."id",
       o."core_id",
       o."provider_id",
       o."target_id",
       o."builtin_relative_path",
       o."sha256",
       o."parser_version",
       o."parse_status",
       o."is_active",
       o."machine_count",
       o."rom_entry_count",
       o."disk_entry_count",
       o."bios_set_count",
       o."default_bios_set_count",
       o."explicit_bios_machine_count",
       o."base_dependency_target_count",
       o."unresolved_relation_count",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."parsed_at_ms",
       o."activated_at_ms"
FROM "dat_versions" o;

INSERT INTO "__new_server_bios_import_items"("server_import_id","requirement_id","requirement_version","core_id","core_name_snapshot","provider_id","target_id","source_kind","logical_name","requirement_mode","condition_code","activation_options_json","delivery_kind","emulator_path","source_version","catalog_digest","dat_version_id","dat_machine_name","expected_size_bytes","expected_md5","expected_sha1","expected_sha256","active_installation_id_snapshot","active_installation_version_snapshot","active_blob_sha256_snapshot","active_status_snapshot","active_validated_requirement_version_snapshot","state","candidate_count","match_method","selection_details_json","previous_installation_id","new_installation_id","outcome_code","created_at_ms","updated_at_ms","completed_at_ms")
SELECT o."server_import_id",
       o."requirement_id",
       o."requirement_version",
       o."core_id",
       o."core_name_snapshot",
       o."provider_id",
       o."target_id",
       o."source_kind",
       o."logical_name",
       o."requirement_mode",
       o."condition_code",
       o."activation_options_json",
       o."delivery_kind",
       o."emulator_path",
       o."source_version",
       o."catalog_digest",
       o."dat_version_id",
       o."dat_machine_name",
       o."expected_size_bytes",
       o."expected_md5",
       o."expected_sha1",
       o."expected_sha256",
       o."active_installation_id_snapshot",
       o."active_installation_version_snapshot",
       o."active_blob_sha256_snapshot",
       o."active_status_snapshot",
       o."active_validated_requirement_version_snapshot",
       o."state",
       o."candidate_count",
       o."match_method",
       o."selection_details_json",
       o."previous_installation_id",
       o."new_installation_id",
       o."outcome_code",
       o."created_at_ms",
       o."updated_at_ms",
       o."completed_at_ms"
FROM "server_bios_import_items" o;

INSERT INTO "__new_import_jobs"("id","upload_session_id","target_platform_instance_id","platform_instance_version","platform_id","default_core_id","provider_id","target_id","dat_version_id","metadata_provider","config_snapshot_json","config_snapshot_digest","state","total_item_count","queued_item_count","running_item_count","review_pending_item_count","published_item_count","discarded_item_count","failed_item_count","cancelled_item_count","ignored_file_count","rejected_file_count","last_error_code","payload_state","payload_release_job_id","payload_released_at_ms","payload_last_error_code","cancel_requested_at_ms","cancel_reason","version","created_at_ms","updated_at_ms","completed_at_ms","resolved_rejected_file_count","reconfigured_from_import_job_id","already_imported_item_count","already_imported_file_count")
SELECT o."id",
       o."upload_session_id",
       o."target_platform_instance_id",
       o."platform_instance_version",
       o."platform_id",
       o."default_core_id",
       o."provider_id",
       o."target_id",
       o."dat_version_id",
       o."metadata_provider",
       o."config_snapshot_json",
       o."config_snapshot_digest",
       o."state",
       o."total_item_count",
       o."queued_item_count",
       o."running_item_count",
       o."review_pending_item_count",
       o."published_item_count",
       o."discarded_item_count",
       o."failed_item_count",
       o."cancelled_item_count",
       o."ignored_file_count",
       o."rejected_file_count",
       o."last_error_code",
       o."payload_state",
       o."payload_release_job_id",
       o."payload_released_at_ms",
       o."payload_last_error_code",
       o."cancel_requested_at_ms",
       o."cancel_reason",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."completed_at_ms",
       o."resolved_rejected_file_count",
       o."reconfigured_from_import_job_id",
       o."already_imported_item_count",
       o."already_imported_file_count"
FROM "import_jobs" o;

INSERT INTO "__new_import_item_core_validations"("id","import_item_id","target_platform_instance_id","platform_instance_version","core_id","provider_id","target_id","prepublish_generation","dat_version_id","default_dos_entry","source_manifest_digest","prepublish_input_digest","status","compatibility_code","dependency_snapshot_json","created_at_ms","source_snapshot_id")
SELECT o."id",
       o."import_item_id",
       o."target_platform_instance_id",
       o."platform_instance_version",
       o."core_id",
       o."provider_id",
       o."target_id",
       o."prepublish_generation",
       o."dat_version_id",
       o."default_dos_entry",
       o."source_manifest_digest",
       o."prepublish_input_digest",
       o."status",
       o."compatibility_code",
       o."dependency_snapshot_json",
       o."created_at_ms",
       o."source_snapshot_id"
FROM "import_item_core_validations" o;

INSERT INTO "__new_import_item_duplicate_matches"("import_item_id","existing_game_id","content_identity_digest","detected_stage","created_at_ms")
SELECT o."import_item_id",
       o."existing_game_id",
       o."content_identity_digest",
       o."detected_stage",
       o."created_at_ms"
FROM "import_item_duplicate_matches" o;

INSERT INTO "__new_review_drafts"("id","import_item_id","target_platform_instance_id","selected_validation_id","selected_candidate_id","cover_candidate_asset_id","background_candidate_asset_id","default_dos_entry","metadata_json","version","created_at_ms","updated_at_ms","cover_uploaded_asset_id","effective_source_snapshot_id")
SELECT o."id",
       o."import_item_id",
       o."target_platform_instance_id",
       o."selected_validation_id",
       o."selected_candidate_id",
       o."cover_candidate_asset_id",
       o."background_candidate_asset_id",
       o."default_dos_entry",
       o."metadata_json",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."cover_uploaded_asset_id",
       o."effective_source_snapshot_id"
FROM "review_drafts" o;

INSERT INTO "__new_rpgmaker_review_profiles"("review_draft_id","generation","evidence_family","evidence_generation","evidence_confidence","engine_version","entry_html_path","file_count","total_bytes","project_fingerprint","requirements_sha256","analysis_json","self_contained_override","provider_id","target_id","dependency_snapshot_sha256","created_at_ms","updated_at_ms")
SELECT o."review_draft_id",
       o."generation",
       o."evidence_family",
       o."evidence_generation",
       o."evidence_confidence",
       o."engine_version",
       o."entry_html_path",
       o."file_count",
       o."total_bytes",
       o."project_fingerprint",
       o."requirements_sha256",
       o."analysis_json",
       o."self_contained_override",
       o."provider_id",
       o."target_id",
       o."dependency_snapshot_sha256",
       o."created_at_ms",
       o."updated_at_ms"
FROM "rpgmaker_review_profiles" o;

INSERT INTO "__new_rpgmaker_runtime_validations"("id","import_item_id","review_version_at_create","effective_source_snapshot_id","project_fingerprint","generation","evidence_generation","evidence_confidence","provider_id","target_id","dependency_snapshot_sha256","launch_id","restore_launch_id","state","last_gate_sequence","machine_gates_json","evidence_screenshot_blob_id","failure_code","decision_note","decided_by_user_id","created_at_ms","updated_at_ms","expires_at_ms","decided_at_ms")
SELECT o."id",
       o."import_item_id",
       o."review_version_at_create",
       o."effective_source_snapshot_id",
       o."project_fingerprint",
       o."generation",
       o."evidence_generation",
       o."evidence_confidence",
       o."provider_id",
       o."target_id",
       o."dependency_snapshot_sha256",
       o."launch_id",
       o."restore_launch_id",
       o."state",
       o."last_gate_sequence",
       o."machine_gates_json",
       o."evidence_screenshot_blob_id",
       o."failure_code",
       o."decision_note",
       o."decided_by_user_id",
       o."created_at_ms",
       o."updated_at_ms",
       o."expires_at_ms",
       o."decided_at_ms"
FROM "rpgmaker_runtime_validations" o;

INSERT INTO "__new_review_arcade_parent_attachments"("id","import_item_id","review_draft_id","base_source_snapshot_id","result_source_snapshot_id","dependency_machine","expected_logical_name","required_by_machine","depth","provider_id","target_id","dat_version_id","upload_file_id","accepted_blob_id","payload_released_at_ms","original_filename","observed_size_bytes","observed_sha256","state","error_code","diagnostics_json","job_id","version","created_at_ms","updated_at_ms","finished_at_ms")
SELECT o."id",
       o."import_item_id",
       o."review_draft_id",
       o."base_source_snapshot_id",
       o."result_source_snapshot_id",
       o."dependency_machine",
       o."expected_logical_name",
       o."required_by_machine",
       o."depth",
       o."provider_id",
       o."target_id",
       o."dat_version_id",
       o."upload_file_id",
       o."accepted_blob_id",
       o."payload_released_at_ms",
       o."original_filename",
       o."observed_size_bytes",
       o."observed_sha256",
       o."state",
       o."error_code",
       o."diagnostics_json",
       o."job_id",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."finished_at_ms"
FROM "review_arcade_parent_attachments" o;

INSERT INTO "__new_review_preview_sessions"("id","import_item_id","source_snapshot_id","validation_id","target_platform_instance_id","provider_id","target_id","bundle_sha256","actor_user_id","idempotency_key","title","content_kind","content_blob_id","content_logical_name","content_format","dependency_snapshot_json","default_dos_entry","emulator_game_id","capture_allowed","credential_sha256","state","bootstrap_expires_at_ms","hard_expires_at_ms","activated_at_ms","finished_at_ms","created_at_ms","updated_at_ms","version")
SELECT o."id",
       o."import_item_id",
       o."source_snapshot_id",
       o."validation_id",
       o."target_platform_instance_id",
       o."provider_id",
       o."target_id",
       o."bundle_sha256",
       o."actor_user_id",
       o."idempotency_key",
       o."title",
       o."content_kind",
       o."content_blob_id",
       o."content_logical_name",
       o."content_format",
       o."dependency_snapshot_json",
       o."default_dos_entry",
       o."emulator_game_id",
       o."capture_allowed",
       o."credential_sha256",
       o."state",
       o."bootstrap_expires_at_ms",
       o."hard_expires_at_ms",
       o."activated_at_ms",
       o."finished_at_ms",
       o."created_at_ms",
       o."updated_at_ms",
       o."version"
FROM "review_preview_sessions" o;

INSERT INTO "__new_review_runtime_screenshots"("id","import_item_id","preview_session_id","source_snapshot_id","validation_id","provider_id","target_id","blob_id","media_type","width_px","height_px","captured_after_ms","captured_at_ms","created_at_ms","updated_at_ms")
SELECT o."id",
       o."import_item_id",
       o."preview_session_id",
       o."source_snapshot_id",
       o."validation_id",
       o."provider_id",
       o."target_id",
       o."blob_id",
       o."media_type",
       o."width_px",
       o."height_px",
       o."captured_after_ms",
       o."captured_at_ms",
       o."created_at_ms",
       o."updated_at_ms"
FROM "review_runtime_screenshots" o;

INSERT INTO "__new_metadata_scrape_runs"("id","import_item_id","game_id","job_id","provider","provider_config_version","state","version","created_at_ms","updated_at_ms","completed_at_ms","error_code")
SELECT o."id",
       o."import_item_id",
       o."game_id",
       o."job_id",
       o."provider",
       o."provider_config_version",
       o."state",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."completed_at_ms",
       o."error_code"
FROM "metadata_scrape_runs" o;

INSERT INTO "__new_jobs"("id","scope_type","scope_id","kind","dedupe_key","execution_no","payload_json","cancellable","state","attempt_count","max_attempts","version","available_at_ms","execution_started_at_ms","execution_deadline_at_ms","leased_until_ms","heartbeat_at_ms","finished_at_ms","worker_id","error_code","error_retryable","cancel_requested_at_ms","cancel_reason","created_at_ms","updated_at_ms")
SELECT o."id",
       o."scope_type",
       o."scope_id",
       CASE o.kind WHEN 'VARIANT_REVALIDATE' THEN 'VARIANT_VALIDATE' WHEN 'GAME_FILE_REVISION' THEN 'GAME_CONTENT_REPLACE' ELSE o.kind END,
       o."dedupe_key",
       o."execution_no",
       o."payload_json",
       o."cancellable",
       o."state",
       o."attempt_count",
       o."max_attempts",
       o."version",
       o."available_at_ms",
       o."execution_started_at_ms",
       o."execution_deadline_at_ms",
       o."leased_until_ms",
       o."heartbeat_at_ms",
       o."finished_at_ms",
       o."worker_id",
       o."error_code",
       o."error_retryable",
       o."cancel_requested_at_ms",
       o."cancel_reason",
       o."created_at_ms",
       o."updated_at_ms"
FROM "jobs" o;

INSERT INTO "__new_upload_consumptions"("id","upload_session_id","upload_file_id","consumer_type","consumer_id","version","released_at_ms","release_reason","created_at_ms")
SELECT o."id",
       o."upload_session_id",
       o."upload_file_id",
       CASE o.consumer_type WHEN 'GAME_FILE_REVISION_JOB' THEN 'GAME_CONTENT_REPLACE_JOB' ELSE o.consumer_type END,
       o."consumer_id",
       o."version",
       o."released_at_ms",
       o."release_reason",
       o."created_at_ms"
FROM "upload_consumptions" o;

INSERT INTO "__new_pegasus_import_collections"("id","import_id","metadata_relative_path","segment_ordinal","name","shortname","description","game_count","issue_count","ignored_rules_json","warning_fields_json","mapping_action","target_platform_instance_id","target_platform_instance_version","target_platform_id","target_default_core_id","target_provider_id","target_id","target_dat_version_id","created_at_ms","updated_at_ms","tag_snapshot_json")
SELECT o."id",
       o."import_id",
       o."metadata_relative_path",
       o."segment_ordinal",
       o."name",
       o."shortname",
       o."description",
       o."game_count",
       o."issue_count",
       o."ignored_rules_json",
       o."warning_fields_json",
       o."mapping_action",
       o."target_platform_instance_id",
       o."target_platform_instance_version",
       o."target_platform_id",
       o."target_default_core_id",
       o."target_provider_id",
       o."target_id",
       o."target_dat_version_id",
       o."created_at_ms",
       o."updated_at_ms",
       o."tag_snapshot_json"
FROM "pegasus_import_collections" o;

INSERT INTO "__new_pegasus_import_items"("id","import_id","collection_id","metadata_relative_path","game_ordinal","source_key","title","discovery_state","execution_state","content_kind","metadata_json","warnings_json","source_manifest_json","source_manifest_digest","discovery_code","error_code","retryable","version","payload_state","payload_release_job_id","payload_released_at_ms","payload_last_error_code","library_import_job_id","library_import_item_id","published_game_id","existing_game_id","existing_matches_json","created_at_ms","updated_at_ms","completed_at_ms","error_details_json")
SELECT o."id",
       o."import_id",
       o."collection_id",
       o."metadata_relative_path",
       o."game_ordinal",
       o."source_key",
       o."title",
       o."discovery_state",
       o."execution_state",
       o."content_kind",
       o."metadata_json",
       o."warnings_json",
       o."source_manifest_json",
       o."source_manifest_digest",
       o."discovery_code",
       o."error_code",
       o."retryable",
       o."version",
       o."payload_state",
       o."payload_release_job_id",
       o."payload_released_at_ms",
       o."payload_last_error_code",
       o."library_import_job_id",
       o."library_import_item_id",
       o."published_game_id",
       o."existing_game_id",
       o."existing_matches_json",
       o."created_at_ms",
       o."updated_at_ms",
       o."completed_at_ms",
       o."error_details_json"
FROM "pegasus_import_items" o;

INSERT INTO "__new_emulationstation_import_collections"("id","import_id","gamelist_relative_path","relative_directory","display_name","game_count","issue_count","folder_entry_count","hidden_game_count","adult_game_count","extension_summary_json","extension_other_count","mapping_action","target_platform_instance_id","target_platform_instance_version","target_platform_id","target_default_core_id","target_provider_id","target_id","target_dat_version_id","tag_snapshot_json","created_at_ms","updated_at_ms")
SELECT o."id",
       o."import_id",
       o."gamelist_relative_path",
       o."relative_directory",
       o."display_name",
       o."game_count",
       o."issue_count",
       o."folder_entry_count",
       o."hidden_game_count",
       o."adult_game_count",
       o."extension_summary_json",
       o."extension_other_count",
       o."mapping_action",
       o."target_platform_instance_id",
       o."target_platform_instance_version",
       o."target_platform_id",
       o."target_default_core_id",
       o."target_provider_id",
       o."target_id",
       o."target_dat_version_id",
       o."tag_snapshot_json",
       o."created_at_ms",
       o."updated_at_ms"
FROM "emulationstation_import_collections" o;

INSERT INTO "__new_emulationstation_import_items"("id","import_id","collection_id","gamelist_relative_path","game_ordinal","source_key","title","source_flags_json","discovery_state","execution_state","content_kind","metadata_json","warnings_json","source_manifest_json","source_manifest_digest","discovery_code","error_code","retryable","version","payload_state","payload_release_job_id","payload_released_at_ms","payload_last_error_code","library_import_job_id","library_import_item_id","published_game_id","existing_game_id","existing_matches_json","error_details_json","created_at_ms","updated_at_ms","completed_at_ms")
SELECT o."id",
       o."import_id",
       o."collection_id",
       o."gamelist_relative_path",
       o."game_ordinal",
       o."source_key",
       o."title",
       o."source_flags_json",
       o."discovery_state",
       o."execution_state",
       o."content_kind",
       o."metadata_json",
       o."warnings_json",
       o."source_manifest_json",
       o."source_manifest_digest",
       o."discovery_code",
       o."error_code",
       o."retryable",
       o."version",
       o."payload_state",
       o."payload_release_job_id",
       o."payload_released_at_ms",
       o."payload_last_error_code",
       o."library_import_job_id",
       o."library_import_item_id",
       o."published_game_id",
       o."existing_game_id",
       o."existing_matches_json",
       o."error_details_json",
       o."created_at_ms",
       o."updated_at_ms",
       o."completed_at_ms"
FROM "emulationstation_import_items" o;

INSERT INTO "__new_launch_sessions"("id","profile_id","purpose","game_id","core_id","provider_id","target_id","bundle_sha256","content_kind","dependency_snapshot_json","compatibility_code","effective_source_snapshot_id","rpgmaker_runtime_validation_id","save_state_id","dos_entry_path","return_to","credential_sha256","state","bootstrap_expires_at_ms","idle_expires_at_ms","activated_at_ms","finished_at_ms","hard_expires_at_ms","created_at_ms","updated_at_ms","version","initial_disc_index","netplay_session_id","netplay_player_no","save_access")
SELECT o."id",
       o."profile_id",
       o."purpose",
       o."game_id",
       COALESCE(v.core_id,(
  SELECT cv.core_id FROM import_item_core_validations cv
  JOIN rpgmaker_runtime_validations rv ON rv.import_item_id=cv.import_item_id
  WHERE rv.id=o.rpgmaker_runtime_validation_id
    AND cv.source_snapshot_id=rv.effective_source_snapshot_id
    AND cv.provider_id=o.provider_id AND cv.target_id=o.target_id
  ORDER BY cv.created_at_ms DESC,cv.id DESC LIMIT 1
)),
       o."provider_id",
       o."target_id",
       o."bundle_sha256",
       COALESCE(c.content_kind,s.content_kind),
       COALESCE(r.dependency_snapshot_json,(
  SELECT cv.dependency_snapshot_json FROM import_item_core_validations cv
  JOIN rpgmaker_runtime_validations rv ON rv.import_item_id=cv.import_item_id
  WHERE rv.id=o.rpgmaker_runtime_validation_id
    AND cv.source_snapshot_id=rv.effective_source_snapshot_id
    AND cv.provider_id=o.provider_id AND cv.target_id=o.target_id
  ORDER BY cv.created_at_ms DESC,cv.id DESC LIMIT 1
)),
       COALESCE(r.compatibility_code,(
  SELECT cv.compatibility_code FROM import_item_core_validations cv
  JOIN rpgmaker_runtime_validations rv ON rv.import_item_id=cv.import_item_id
  WHERE rv.id=o.rpgmaker_runtime_validation_id
    AND cv.source_snapshot_id=rv.effective_source_snapshot_id
    AND cv.provider_id=o.provider_id AND cv.target_id=o.target_id
  ORDER BY cv.created_at_ms DESC,cv.id DESC LIMIT 1
)),
       o."effective_source_snapshot_id",
       o."rpgmaker_runtime_validation_id",
       o."save_state_id",
       o."dos_entry_path",
       o."return_to",
       o."credential_sha256",
       o."state",
       o."bootstrap_expires_at_ms",
       o."idle_expires_at_ms",
       o."activated_at_ms",
       o."finished_at_ms",
       o."hard_expires_at_ms",
       o."created_at_ms",
       o."updated_at_ms",
       o."version",
       o."initial_disc_index",
       o."netplay_session_id",
       o."netplay_player_no",
       o."save_access"
FROM launch_sessions o LEFT JOIN game_variant_revisions r ON r.id=o.game_variant_revision_id LEFT JOIN game_variants v ON v.id=r.game_variant_id LEFT JOIN game_content_revisions c ON c.id=o.game_content_revision_id LEFT JOIN import_item_source_snapshots s ON s.id=o.effective_source_snapshot_id;

INSERT INTO "__new_launch_external_files"("launch_session_id","virtual_path","logical_name","blob_id","created_at_ms","kind")
SELECT o."launch_session_id",
       o."virtual_path",
       o."logical_name",
       o."blob_id",
       o."created_at_ms",
       o."kind"
FROM "launch_external_files" o;

INSERT INTO "__new_save_states"("id","profile_id","game_id","checkpoint_format","payload_blob_id","payload_sha256","payload_size_bytes","screenshot_blob_id","name","active_duration_ms","dos_entry_path","version","created_at_ms","updated_at_ms","deleted_at_ms","source_launch_session_id","disc_index")
SELECT o."id",
       o."profile_id",
       o."game_id",
       o."checkpoint_format",
       o."payload_blob_id",
       o."payload_sha256",
       o."payload_size_bytes",
       o."screenshot_blob_id",
       o."name",
       o."active_duration_ms",
       o."dos_entry_path",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."deleted_at_ms",
       o."source_launch_session_id",
       o."disc_index"
FROM "save_states" o;

INSERT INTO "__new_play_sessions"("id","launch_session_id","profile_id","game_id","started_at_ms","last_heartbeat_at_ms","ended_at_ms","active_duration_ms","last_client_sequence","state","version","created_at_ms","updated_at_ms")
SELECT o."id",
       o."launch_session_id",
       o."profile_id",
       o."game_id",
       o."started_at_ms",
       o."last_heartbeat_at_ms",
       o."ended_at_ms",
       o."active_duration_ms",
       o."last_client_sequence",
       o."state",
       o."version",
       o."created_at_ms",
       o."updated_at_ms"
FROM "play_sessions" o;

INSERT INTO "__new_netplay_rooms"("id","host_profile_id","state","selected_game_id","selected_game_variant_id","netplay_profile_id","profile_digest","max_players","current_session_id","version","expires_at_ms","created_at_ms","updated_at_ms","ended_at_ms","end_reason")
SELECT o."id",
       o."host_profile_id",
       o."state",
       o."selected_game_id",
       r.game_variant_id,
       o."netplay_profile_id",
       o."profile_digest",
       o."max_players",
       o."current_session_id",
       o."version",
       o."expires_at_ms",
       o."created_at_ms",
       o."updated_at_ms",
       o."ended_at_ms",
       o."end_reason"
FROM netplay_rooms o LEFT JOIN game_variant_revisions r ON r.id=o.selected_game_variant_revision_id;

INSERT INTO "__new_netplay_sessions"("id","room_id","session_no","state","game_id","game_variant_id","provider_id","target_id","bundle_sha256","netplay_profile_id","profile_json","profile_digest","player_count","occupied_seat_mask","authority_player_no","resync_count","version","created_at_ms","updated_at_ms","started_at_ms","finished_at_ms","end_reason")
SELECT o."id",
       o."room_id",
       o."session_no",
       o."state",
       o."game_id",
       r.game_variant_id,
       o."provider_id",
       o."target_id",
       (SELECT p.bundle_sha256 FROM runtime_providers p WHERE p.provider_id=o.provider_id),
       o."netplay_profile_id",
       o."profile_json",
       o."profile_digest",
       o."player_count",
       o."occupied_seat_mask",
       o."authority_player_no",
       o."resync_count",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."started_at_ms",
       o."finished_at_ms",
       o."end_reason"
FROM netplay_sessions o JOIN game_variant_revisions r ON r.id=o.game_variant_revision_id;

INSERT INTO "__new_game_assets"("id","game_id","blob_id","kind","ordinal","width_px","height_px","media_type","created_at_ms")
SELECT o."id",
       o."game_id",
       o."blob_id",
       o."kind",
       o."ordinal",
       o."width_px",
       o."height_px",
       o."media_type",
       o."created_at_ms"
FROM game_assets o JOIN games g ON g.id=o.game_id AND g.current_metadata_revision_id=o.metadata_revision_id;

INSERT INTO "__new_game_variants"("id","game_id","core_id","provider_id","target_id","dat_version_id","emulator_game_id","status","compatibility_code","dependency_snapshot_json","default_dos_entry","version","created_at_ms","updated_at_ms")
SELECT o."id",
       o."game_id",
       o."core_id",
       r.provider_id,
       r.target_id,
       r.dat_version_id,
       r.emulator_game_id,
       r.status,
       r.compatibility_code,
       r.dependency_snapshot_json,
       r.default_dos_entry,
       o."version",
       o."created_at_ms",
       o."updated_at_ms"
FROM game_variants o JOIN game_variant_revisions r ON r.id=o.current_revision_id;

INSERT INTO "__new_games"("id","platform_instance_id","title","title_initial","description","developer","publisher","genre","players","release_year","metadata_source_kind","metadata_source_ref_id","content_kind","content_source_kind","content_source_ref_id","source_manifest_json","source_manifest_digest","status","payload_state","payload_release_job_id","payload_released_at_ms","payload_last_error_code","search_text","version","created_at_ms","updated_at_ms","deleted_at_ms")
SELECT o."id",
       o."platform_instance_id",
       m.title,
       m.title_initial,
       m.description,
       m.developer,
       m.publisher,
       m.genre,
       m.players,
       m.release_year,
       m.source_kind,
       m.source_ref_id,
       c.content_kind,
       c.source_kind,
       c.source_ref_id,
       c.source_manifest_json,
       c.source_manifest_digest,
       o."status",
       o."payload_state",
       o."payload_release_job_id",
       o."payload_released_at_ms",
       o."payload_last_error_code",
       o."search_text",
       o."version",
       o."created_at_ms",
       o."updated_at_ms",
       o."deleted_at_ms"
FROM "games" o JOIN game_metadata_revisions m ON m.id=o.current_metadata_revision_id JOIN game_content_revisions c ON c.id=o.current_content_revision_id;

INSERT INTO "__new_rpgmaker_variant_profiles"("game_variant_id","generation","dependency_snapshot_sha256","runtime_validation_id")
SELECT v.id,
       o."generation",
       o."dependency_snapshot_sha256",
       o."runtime_validation_id"
FROM rpgmaker_variant_profiles o JOIN game_variants v ON v.current_revision_id=o.game_variant_revision_id;

INSERT INTO "__new_variant_dependencies"("game_variant_id","kind","logical_archive","dat_version_id","source_machine_name","required_entries_json","state","created_at_ms")
SELECT v.id,
       o."kind",
       o."logical_archive",
       o."dat_version_id",
       o."source_machine_name",
       o."required_entries_json",
       o."state",
       o."created_at_ms"
FROM variant_dependencies o JOIN game_variants v ON v.current_revision_id=o.game_variant_revision_id;

INSERT INTO "__new_variant_files"("game_variant_id","role","logical_name","blob_id","sort_order")
SELECT v.id,
       o."role",
       o."logical_name",
       o."blob_id",
       o."sort_order"
FROM variant_files o JOIN game_variants v ON v.current_revision_id=o.game_variant_revision_id;

INSERT INTO "__new_dos_entries"("game_id","normalized_path","original_relative_path","kind","rank","enabled","direct_launch_safe")
SELECT g.id,
       o."normalized_path",
       o."original_relative_path",
       o."kind",
       o."rank",
       o."enabled",
       o."direct_launch_safe"
FROM dos_entries o JOIN games g ON g.current_content_revision_id=o.game_content_revision_id;

INSERT INTO "__new_game_files"("game_id","role","logical_name","blob_id","source_archive_blob_id","source_archive_entry_ordinal","sort_order")
SELECT g.id,
       o."role",
       o."logical_name",
       o."blob_id",
       o."source_archive_blob_id",
       o."source_archive_entry_ordinal",
       o."sort_order"
FROM game_content_files o JOIN games g ON g.current_content_revision_id=o.game_content_revision_id;

INSERT INTO "__new_game_variant_runtime_packs"("game_variant_id","slot","declared_name","normalized_declared_name","definition_id","installation_id")
SELECT v.id,
       o."slot",
       o."declared_name",
       o."normalized_declared_name",
       o."definition_id",
       o."installation_id"
FROM game_variant_revision_runtime_packs o JOIN game_variants v ON v.current_revision_id=o.game_variant_revision_id;

INSERT INTO "__new_rpgmaker_game_profiles"("game_id","evidence_family","evidence_generation","evidence_confidence","engine_version","entry_html_path","file_count","total_bytes","project_fingerprint","requirements_sha256","analysis_json","created_at_ms","updated_at_ms")
SELECT g.id,
       o."evidence_family",
       o."evidence_generation",
       o."evidence_confidence",
       o."engine_version",
       o."entry_html_path",
       o."file_count",
       o."total_bytes",
       o."project_fingerprint",
       o."requirements_sha256",
       o."analysis_json",
       o."created_at_ms",
       o.created_at_ms
FROM rpgmaker_content_profiles o JOIN games g ON g.current_content_revision_id=o.content_revision_id;

DROP TABLE "launch_external_files";
DROP TABLE "save_states";
DROP TABLE "play_sessions";
DROP TABLE "netplay_sessions";
DROP TABLE "netplay_rooms";
DROP TABLE "rpgmaker_runtime_validations";
DROP TABLE "launch_sessions";
DROP TABLE "review_runtime_screenshots";
DROP TABLE "review_preview_sessions";
DROP TABLE "review_arcade_parent_attachments";
DROP TABLE "rpgmaker_review_profiles";
DROP TABLE "review_drafts";
DROP TABLE "server_bios_import_items";
DROP TABLE "emulationstation_import_items";
DROP TABLE "emulationstation_import_collections";
DROP TABLE "pegasus_import_items";
DROP TABLE "pegasus_import_collections";
DROP TABLE "metadata_scrape_runs";
DROP TABLE "import_item_duplicate_matches";
DROP TABLE "import_item_core_validations";
DROP TABLE "import_jobs";
DROP TABLE "game_variant_revision_runtime_packs";
DROP TABLE "rpgmaker_variant_profiles";
DROP TABLE "variant_dependencies";
DROP TABLE "variant_files";
DROP TABLE "game_variant_revisions";
DROP TABLE "game_variants";
DROP TABLE "game_assets";
DROP TABLE "dos_entries";
DROP TABLE "game_content_files";
DROP TABLE "rpgmaker_content_profiles";
DROP TABLE "game_content_revisions";
DROP TABLE "game_metadata_revisions";
DROP TABLE "games";
DROP TABLE "bios_requirements";
DROP TABLE "dat_versions";
DROP TABLE "runtime_targets";
DROP TABLE "upload_consumptions";
DROP TABLE "jobs";

ALTER TABLE "__new_jobs" RENAME TO "jobs";
ALTER TABLE "__new_upload_consumptions" RENAME TO "upload_consumptions";
ALTER TABLE "__new_runtime_targets" RENAME TO "runtime_targets";
ALTER TABLE "__new_bios_requirements" RENAME TO "bios_requirements";
ALTER TABLE "__new_dat_versions" RENAME TO "dat_versions";
ALTER TABLE "__new_games" RENAME TO "games";
ALTER TABLE "__new_game_assets" RENAME TO "game_assets";
ALTER TABLE "__new_game_files" RENAME TO "game_files";
ALTER TABLE "__new_game_variants" RENAME TO "game_variants";
ALTER TABLE "__new_game_variant_runtime_packs" RENAME TO "game_variant_runtime_packs";
ALTER TABLE "__new_rpgmaker_game_profiles" RENAME TO "rpgmaker_game_profiles";
ALTER TABLE "__new_rpgmaker_variant_profiles" RENAME TO "rpgmaker_variant_profiles";
ALTER TABLE "__new_variant_dependencies" RENAME TO "variant_dependencies";
ALTER TABLE "__new_variant_files" RENAME TO "variant_files";
ALTER TABLE "__new_dos_entries" RENAME TO "dos_entries";
ALTER TABLE "__new_import_jobs" RENAME TO "import_jobs";
ALTER TABLE "__new_import_item_core_validations" RENAME TO "import_item_core_validations";
ALTER TABLE "__new_import_item_duplicate_matches" RENAME TO "import_item_duplicate_matches";
ALTER TABLE "__new_review_drafts" RENAME TO "review_drafts";
ALTER TABLE "__new_rpgmaker_review_profiles" RENAME TO "rpgmaker_review_profiles";
ALTER TABLE "__new_rpgmaker_runtime_validations" RENAME TO "rpgmaker_runtime_validations";
ALTER TABLE "__new_review_arcade_parent_attachments" RENAME TO "review_arcade_parent_attachments";
ALTER TABLE "__new_review_preview_sessions" RENAME TO "review_preview_sessions";
ALTER TABLE "__new_review_runtime_screenshots" RENAME TO "review_runtime_screenshots";
ALTER TABLE "__new_metadata_scrape_runs" RENAME TO "metadata_scrape_runs";
ALTER TABLE "__new_server_bios_import_items" RENAME TO "server_bios_import_items";
ALTER TABLE "__new_pegasus_import_collections" RENAME TO "pegasus_import_collections";
ALTER TABLE "__new_pegasus_import_items" RENAME TO "pegasus_import_items";
ALTER TABLE "__new_emulationstation_import_collections" RENAME TO "emulationstation_import_collections";
ALTER TABLE "__new_emulationstation_import_items" RENAME TO "emulationstation_import_items";
ALTER TABLE "__new_netplay_rooms" RENAME TO "netplay_rooms";
ALTER TABLE "__new_netplay_sessions" RENAME TO "netplay_sessions";
ALTER TABLE "__new_launch_sessions" RENAME TO "launch_sessions";
ALTER TABLE "__new_launch_external_files" RENAME TO "launch_external_files";
ALTER TABLE "__new_save_states" RENAME TO "save_states";
ALTER TABLE "__new_play_sessions" RENAME TO "play_sessions";

INSERT OR IGNORE INTO runtime_target_bindings SELECT * FROM __runtime_target_bindings_backup;
DROP TABLE __runtime_target_bindings_backup;

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

CREATE INDEX fk_launch_validation
ON launch_sessions(rpgmaker_runtime_validation_id,purpose,state);

CREATE INDEX fk_platform_instances_default_core ON platform_instances(default_core_id);

CREATE INDEX fk_runtime_asset_pack_files_blob ON runtime_asset_pack_files(blob_id);

CREATE INDEX fk_runtime_asset_pack_installations_bundle ON runtime_asset_pack_installations(bundle_blob_id);

CREATE INDEX fk_runtime_asset_pack_installations_definition
ON runtime_asset_pack_installations(definition_id,status,created_at_ms,id);

CREATE INDEX fk_upload_files_session ON upload_files(upload_session_id);

CREATE INDEX game_files_game ON game_files(game_id,sort_order,logical_name);

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

CREATE INDEX rpgmaker_validation_gate_launch
ON rpgmaker_runtime_validation_gate_events(launch_id,sequence);

CREATE UNIQUE INDEX rpgmaker_validation_gate_terminal
ON rpgmaker_runtime_validation_gate_events(validation_id,gate)
WHERE phase IN ('PASS','FAIL');

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

CREATE TRIGGER bios_installations_payload_terminal
BEFORE UPDATE OF blob_id,payload_released_at_ms ON bios_installations
WHEN OLD.blob_id IS NULL AND NEW.blob_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'released BIOS payload is terminal'); END;

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

CREATE TRIGGER bios_requirements_runtime_target_insert
BEFORE INSERT ON bios_requirements
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

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

CREATE TRIGGER dat_versions_runtime_target_insert
BEFORE INSERT ON dat_versions
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER dos_entries_immutable_delete BEFORE DELETE ON dos_entries BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER dos_entries_immutable_update BEFORE UPDATE ON dos_entries BEGIN SELECT RAISE(ABORT, 'immutable'); END;

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

CREATE TRIGGER emulationstation_collection_delete
BEFORE DELETE ON emulationstation_import_collections
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import
  WHERE import.id=OLD.import_id AND import.state IN ('SCANNING','AWAITING_MAPPING','EXPIRED'))
BEGIN SELECT RAISE(ABORT,'EmulationStation collection snapshot is frozen'); END;

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

CREATE TRIGGER emulationstation_gamelist_delete
BEFORE DELETE ON emulationstation_import_gamelists
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import
  WHERE import.id=OLD.import_id AND import.state IN ('SCANNING','AWAITING_MAPPING','EXPIRED'))
BEGIN SELECT RAISE(ABORT,'EmulationStation gamelist snapshot is frozen'); END;

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

CREATE TRIGGER emulationstation_gamelists_immutable_update
BEFORE UPDATE ON emulationstation_import_gamelists
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER emulationstation_import_collections_runtime_target_update
BEFORE UPDATE OF mapping_action,target_provider_id,target_id
ON emulationstation_import_collections
WHEN NEW.mapping_action='IMPORT' AND NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.target_provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

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

CREATE TRIGGER emulationstation_import_identity_update
BEFORE UPDATE OF id,root_id,root_label_snapshot,source_relative_path,root_config_digest,release_year_max,
  scan_job_id,created_by_user_id,created_at_ms,expires_at_ms ON emulationstation_imports
WHEN NEW.id<>OLD.id OR NEW.root_id<>OLD.root_id OR NEW.root_label_snapshot<>OLD.root_label_snapshot
  OR NEW.source_relative_path<>OLD.source_relative_path OR NEW.root_config_digest<>OLD.root_config_digest
  OR NEW.release_year_max<>OLD.release_year_max OR NEW.scan_job_id<>OLD.scan_job_id
  OR NEW.created_by_user_id<>OLD.created_by_user_id OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.expires_at_ms<>OLD.expires_at_ms
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation import identity'); END;

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

CREATE TRIGGER emulationstation_import_scan_job_insert
BEFORE INSERT ON emulationstation_imports
WHEN NOT EXISTS(
  SELECT 1 FROM jobs job WHERE job.id=NEW.scan_job_id
  AND job.kind='SERVER_EMULATIONSTATION_SCAN'
  AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=NEW.id
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation scan job'); END;

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

CREATE TRIGGER emulationstation_import_version_update
BEFORE UPDATE ON emulationstation_imports
WHEN NEW.version<OLD.version OR NEW.mapping_version<OLD.mapping_version OR NEW.updated_at_ms<OLD.updated_at_ms
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation import version'); END;

CREATE TRIGGER emulationstation_item_delete
BEFORE DELETE ON emulationstation_import_items
WHEN NOT EXISTS(SELECT 1 FROM emulationstation_imports import WHERE import.id=OLD.import_id AND (
  import.state='SCANNING' OR import.state='AWAITING_MAPPING' AND OLD.execution_state='PENDING'
  OR import.state='EXPIRED' AND OLD.execution_state='CANCELLED'
))
BEGIN SELECT RAISE(ABORT,'EmulationStation item snapshot is frozen'); END;

CREATE TRIGGER emulationstation_item_execution_update
BEFORE UPDATE OF execution_state,error_code,retryable,library_import_job_id,library_import_item_id,
  published_game_id,existing_game_id,error_details_json,completed_at_ms
ON emulationstation_import_items
WHEN (NEW.library_import_job_id IS NULL)<>(NEW.library_import_item_id IS NULL)
  OR NEW.execution_state='PUBLISHED' AND (NEW.published_game_id IS NULL OR NEW.existing_game_id IS NOT NULL)
  OR NEW.execution_state<>'PUBLISHED' AND NEW.published_game_id IS NOT NULL
  OR NEW.execution_state='SKIPPED_EXISTING' AND NEW.existing_game_id IS NULL
  OR NEW.execution_state<>'SKIPPED_EXISTING' AND NEW.existing_game_id IS NOT NULL
  OR NEW.retryable=1 AND NEW.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
  OR NEW.error_details_json IS NOT NULL AND NEW.execution_state NOT IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item execution'); END;

CREATE TRIGGER emulationstation_item_insert
BEFORE INSERT ON emulationstation_import_items
WHEN NEW.execution_state<>'PENDING' OR NEW.payload_state<>'RETAINED'
  OR NEW.library_import_job_id IS NOT NULL OR NEW.library_import_item_id IS NOT NULL
  OR NEW.published_game_id IS NOT NULL OR NEW.existing_game_id IS NOT NULL
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
    WHERE entry.type<>'object' OR (SELECT count(*) FROM json_each(entry.value))<>1
      OR EXISTS(SELECT 1 FROM json_each(entry.value) member WHERE member.key<>'gameId' OR member.type='null')
      OR json_type(entry.value,'$.gameId')<>'text' OR game.id IS NULL
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

CREATE TRIGGER emulationstation_item_published_update
BEFORE UPDATE OF execution_state,published_game_id ON emulationstation_import_items
WHEN NEW.execution_state='PUBLISHED' AND (
  NEW.published_game_id IS NULL OR NOT EXISTS(
    SELECT 1 FROM games game
    WHERE game.id=NEW.published_game_id AND game.metadata_source_kind='SERVER_EMULATIONSTATION_IMPORT'
    AND game.metadata_source_ref_id=NEW.id AND game.content_source_kind='SERVER_EMULATIONSTATION_IMPORT'
    AND game.content_source_ref_id=NEW.id
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

CREATE TRIGGER emulationstation_item_snapshot_update
BEFORE UPDATE OF import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,source_flags_json,
  discovery_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,discovery_code,created_at_ms
ON emulationstation_import_items
BEGIN SELECT RAISE(ABORT,'immutable EmulationStation item snapshot'); END;

CREATE TRIGGER emulationstation_item_state_update
BEFORE UPDATE OF execution_state ON emulationstation_import_items
WHEN OLD.execution_state<>NEW.execution_state AND NOT (
  OLD.execution_state='PENDING' AND NEW.execution_state IN ('COPYING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='COPYING' AND NEW.execution_state IN ('VALIDATING','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='VALIDATING' AND NEW.execution_state IN ('REVIEW_PENDING','SKIPPED_EXISTING','BLOCKED_CONTENT','COMMIT_FAILED','CANCELLED') OR
  OLD.execution_state='REVIEW_PENDING' AND NEW.execution_state IN ('PUBLISHED','REVIEW_DISCARDED') OR
  OLD.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED') AND OLD.retryable=1 AND NEW.execution_state='PENDING'
)
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item state transition'); END;

CREATE TRIGGER emulationstation_item_version_update
BEFORE UPDATE ON emulationstation_import_items
WHEN NEW.version<OLD.version OR NEW.updated_at_ms<OLD.updated_at_ms
BEGIN SELECT RAISE(ABORT,'invalid EmulationStation item version'); END;

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

CREATE TRIGGER game_assets_owner_insert
BEFORE INSERT ON game_assets
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER game_assets_owner_update
BEFORE UPDATE OF game_id ON game_assets
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER game_files_owner_insert
BEFORE INSERT ON game_files
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER game_files_owner_update
BEFORE UPDATE OF game_id ON game_files
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER game_variant_runtime_packs_validate_insert
BEFORE INSERT ON game_variant_runtime_packs
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_variant_profiles profile
  JOIN runtime_asset_pack_definitions definition ON definition.id=NEW.definition_id
  JOIN runtime_asset_pack_installations installation
    ON installation.id=NEW.installation_id AND installation.definition_id=definition.id
  WHERE profile.game_variant_id=NEW.game_variant_id
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

CREATE TRIGGER game_variant_runtime_packs_validate_update
BEFORE UPDATE ON game_variant_runtime_packs
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_variant_profiles profile
  JOIN runtime_asset_pack_definitions definition ON definition.id=NEW.definition_id
  JOIN runtime_asset_pack_installations installation
    ON installation.id=NEW.installation_id AND installation.definition_id=definition.id
  WHERE profile.game_variant_id=NEW.game_variant_id
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

CREATE TRIGGER game_variants_guarded_update
BEFORE UPDATE ON game_variants
WHEN NEW.id<>OLD.id OR NEW.game_id<>OLD.game_id OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.version<>OLD.version+1 OR NEW.updated_at_ms<OLD.updated_at_ms
  OR NOT EXISTS(
    SELECT 1 FROM runtime_target_bindings binding
    WHERE binding.core_id=NEW.core_id AND binding.provider_id=NEW.provider_id
      AND binding.target_id=NEW.target_id AND binding.launch_policy<>'DISABLED'
  )
BEGIN SELECT RAISE(ABORT,'invalid current game runtime settings update'); END;

CREATE TRIGGER game_variants_owner_insert
BEFORE INSERT ON game_variants
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
  OR NOT EXISTS(
    SELECT 1 FROM runtime_target_bindings binding
    WHERE binding.core_id=NEW.core_id AND binding.provider_id=NEW.provider_id
      AND binding.target_id=NEW.target_id AND binding.launch_policy<>'DISABLED'
  )
BEGIN SELECT RAISE(ABORT,'invalid current game runtime settings'); END;

CREATE TRIGGER game_variants_runtime_target_insert
BEFORE INSERT ON game_variants
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER games_deleted_is_terminal
BEFORE UPDATE OF status ON games
WHEN OLD.status='DELETED' AND NEW.status<>'DELETED'
BEGIN SELECT RAISE(ABORT,'deleted game is terminal'); END;

CREATE TRIGGER games_guarded_update
BEFORE UPDATE ON games
WHEN NEW.id<>OLD.id
  OR NEW.created_at_ms<>OLD.created_at_ms
  OR NEW.version<>OLD.version+1
  OR NEW.updated_at_ms<OLD.updated_at_ms
BEGIN SELECT RAISE(ABORT,'invalid current game update'); END;

CREATE TRIGGER import_group_requests_immutable_delete
BEFORE DELETE ON import_group_requests
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_group_requests_immutable_update
BEFORE UPDATE ON import_group_requests
BEGIN SELECT RAISE(ABORT,'immutable'); END;

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

CREATE TRIGGER import_item_core_validations_runtime_target_insert
BEFORE INSERT ON import_item_core_validations
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

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
  WHERE snapshot.id=NEW.source_snapshot_id AND snapshot.content_kind='MULTI_DISC'
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

CREATE TRIGGER import_item_validation_files_immutable_delete
BEFORE DELETE ON import_item_validation_files
WHEN NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation JOIN import_items item ON item.id=validation.import_item_id
  WHERE validation.id=OLD.import_item_core_validation_id AND item.payload_state IN ('RELEASING','FAILED')
)
BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_item_validation_files_immutable_update
BEFORE UPDATE ON import_item_validation_files BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER import_items_review_handoff_kind_immutable
BEFORE UPDATE OF review_handoff_kind ON import_items
WHEN NEW.review_handoff_kind<>OLD.review_handoff_kind
BEGIN SELECT RAISE(ABORT,'immutable import review handoff kind'); END;

CREATE TRIGGER import_job_file_resolutions_immutable_delete
BEFORE DELETE ON import_job_file_resolutions BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER import_job_file_resolutions_immutable_update
BEFORE UPDATE ON import_job_file_resolutions BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER import_jobs_runtime_target_insert
BEFORE INSERT ON import_jobs
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER instance_state_default_password_no_reenable
BEFORE UPDATE OF test_default_password_active ON instance_state
WHEN OLD.state='COMPLETED' AND OLD.test_default_password_active=0 AND NEW.test_default_password_active=1
BEGIN SELECT RAISE(ABORT, 'test default password cannot be re-enabled'); END;

CREATE TRIGGER instance_state_no_reopen
BEFORE UPDATE OF state ON instance_state
WHEN OLD.state='COMPLETED' AND NEW.state!='COMPLETED'
BEGIN SELECT RAISE(ABORT, 'initialization is terminal'); END;

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

CREATE TRIGGER isolated_runtime_capabilities_immutable_delete
BEFORE DELETE ON isolated_runtime_capabilities
WHEN OLD.launch_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM review_preview_sessions preview
  WHERE preview.id=OLD.preview_id AND preview.state NOT IN ('EXPIRED','REVOKED')
)
BEGIN SELECT RAISE(ABORT,'isolated runtime capability is retained for audit'); END;

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

CREATE TRIGGER job_events_immutable_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER job_events_immutable_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT,'immutable'); END;

CREATE TRIGGER job_input_snapshots_immutable_delete BEFORE DELETE ON job_input_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER job_input_snapshots_immutable_update BEFORE UPDATE ON job_input_snapshots BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_content_files_immutable_delete
BEFORE DELETE ON launch_content_files
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  WHERE launch.id=OLD.launch_session_id AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
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
  WHERE launch.id=OLD.launch_session_id AND launch.state IN ('FINISHED','EXPIRED','REVOKED')
)
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_immutable_update
BEFORE UPDATE ON launch_external_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER launch_external_files_kind_insert
BEFORE INSERT ON launch_external_files
WHEN NEW.kind='DISC' AND NOT EXISTS(
  SELECT 1 FROM launch_content_files content
  WHERE content.launch_session_id=NEW.launch_session_id
  AND content.format_version='RETROM_MULTIDISC_M3U_V1'
)
BEGIN SELECT RAISE(ABORT,'disc external file requires multi-disc launch content'); END;

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

CREATE TRIGGER launch_sessions_netplay_immutable
BEFORE UPDATE OF netplay_session_id,netplay_player_no,save_access ON launch_sessions
BEGIN SELECT RAISE(ABORT,'immutable netplay launch binding'); END;

CREATE TRIGGER launch_sessions_revoke_isolated_runtime
AFTER UPDATE OF state ON launch_sessions
WHEN NEW.state IN ('FINISHED','EXPIRED','REVOKED') AND OLD.state<>NEW.state
BEGIN
  UPDATE isolated_runtime_capabilities SET revoked_at_ms=NEW.finished_at_ms
  WHERE launch_id=NEW.id AND revoked_at_ms IS NULL;
END;

CREATE TRIGGER launch_sessions_runtime_target_immutable
BEFORE UPDATE OF provider_id,target_id,bundle_sha256
ON launch_sessions
BEGIN SELECT RAISE(ABORT,'immutable runtime target snapshot'); END;

CREATE TRIGGER launch_sessions_runtime_target_insert
BEFORE INSERT ON launch_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
    AND provider.bundle_sha256=NEW.bundle_sha256
)
OR NEW.purpose='PRODUCT' AND NOT EXISTS(
  SELECT 1 FROM games game
  JOIN game_variants variant ON variant.game_id=game.id
  WHERE game.id=NEW.game_id AND game.status='PUBLISHED'
    AND variant.core_id=NEW.core_id AND variant.provider_id=NEW.provider_id
    AND variant.target_id=NEW.target_id AND variant.status='READY'
)
OR NEW.purpose='RPG_RUNTIME_VALIDATION' AND NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validations validation
  WHERE validation.id=NEW.rpgmaker_runtime_validation_id
    AND validation.effective_source_snapshot_id=NEW.effective_source_snapshot_id
    AND validation.provider_id=NEW.provider_id AND validation.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

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

CREATE TRIGGER netplay_rooms_game_variant_insert
BEFORE INSERT ON netplay_rooms
WHEN NEW.selected_game_variant_id IS NOT NULL AND NOT EXISTS(
  SELECT 1
  FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=NEW.selected_game_variant_id AND variant.game_id=NEW.selected_game_id
    AND variant.status='READY' AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'invalid netplay game variant'); END;

CREATE TRIGGER netplay_rooms_game_variant_update
BEFORE UPDATE OF selected_game_id,selected_game_variant_id ON netplay_rooms
WHEN NEW.selected_game_variant_id IS NOT NULL AND NOT EXISTS(
  SELECT 1
  FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=NEW.selected_game_variant_id AND variant.game_id=NEW.selected_game_id
    AND variant.status='READY' AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'invalid netplay game variant'); END;

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
BEFORE UPDATE OF selected_game_id,selected_game_variant_id,netplay_profile_id,profile_digest,max_players
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

CREATE TRIGGER netplay_sessions_runtime_target_immutable
BEFORE UPDATE OF provider_id,target_id,bundle_sha256
ON netplay_sessions
BEGIN SELECT RAISE(ABORT,'immutable runtime netplay snapshot'); END;

CREATE TRIGGER netplay_sessions_runtime_target_insert
BEFORE INSERT ON netplay_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
    AND provider.bundle_sha256=NEW.bundle_sha256
    AND json_extract(target.capabilities_json,'$.netplayPort')=1
)
BEGIN SELECT RAISE(ABORT,'invalid runtime netplay snapshot'); END;

CREATE TRIGGER netplay_sessions_validate_insert
BEFORE INSERT ON netplay_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM netplay_rooms room
  JOIN game_variants variant ON variant.id=NEW.game_variant_id
  JOIN games game ON game.id=variant.game_id
  WHERE room.id=NEW.room_id AND room.selected_game_id=NEW.game_id
    AND room.selected_game_variant_id=NEW.game_variant_id
    AND room.netplay_profile_id=NEW.netplay_profile_id AND room.profile_digest=NEW.profile_digest
    AND variant.game_id=NEW.game_id AND variant.status='READY' AND game.status='PUBLISHED'
    AND variant.provider_id=NEW.provider_id AND variant.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid netplay session snapshot'); END;

CREATE TRIGGER pegasus_asset_snapshot_update
BEFORE UPDATE ON pegasus_import_item_assets
WHEN NEW.item_id<>OLD.item_id OR NEW.kind<>OLD.kind OR NEW.resolution_method<>OLD.resolution_method OR
  NEW.relative_path<>OLD.relative_path OR NEW.size_bytes IS NOT OLD.size_bytes OR
  NEW.source_facts_digest IS NOT OLD.source_facts_digest OR NEW.created_at_ms<>OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable Pegasus asset snapshot'); END;

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

CREATE TRIGGER pegasus_import_collections_runtime_target_update
BEFORE UPDATE OF mapping_action,target_provider_id,target_id
ON pegasus_import_collections
WHEN NEW.mapping_action='IMPORT' AND NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.target_provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

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
    WHERE game.id=NEW.published_game_id AND game.metadata_source_kind='SERVER_PEGASUS_IMPORT'
    AND game.metadata_source_ref_id=NEW.id AND game.content_source_kind='SERVER_PEGASUS_IMPORT'
    AND game.content_source_ref_id=NEW.id
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
screenshot_only_count,duplicate_count,attachment_active_count,source_flagged_count,
created_by_user_id,created_at_ms ON review_bulk_approvals
BEGIN SELECT RAISE(ABORT,'immutable review bulk approval input'); END;

CREATE TRIGGER review_draft_runtime_pack_selections_validate_delete
BEFORE DELETE ON review_draft_runtime_pack_selections
WHEN NOT EXISTS(
  SELECT 1 FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id
  WHERE draft.id=OLD.review_draft_id AND item.state='REVIEW_PENDING'
)
BEGIN SELECT RAISE(ABORT,'finalized review runtime pack selection'); END;

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
  AND snapshot.import_item_id=NEW.import_item_id AND snapshot.content_kind='MULTI_DISC'
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
  AND snapshot.content_kind='MULTI_DISC' AND snapshot.created_by='MULTI_DISC_ATTACHMENT'
  AND snapshot.id<>NEW.base_source_snapshot_id
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
)
BEGIN SELECT RAISE(ABORT,'invalid review preview project file'); END;

CREATE TRIGGER review_preview_sessions_revoke_isolated_runtime
AFTER UPDATE OF state ON review_preview_sessions
WHEN NEW.state IN ('EXPIRED','REVOKED') AND OLD.state<>NEW.state
BEGIN
  UPDATE isolated_runtime_capabilities SET revoked_at_ms=NEW.finished_at_ms
  WHERE preview_id=NEW.id AND revoked_at_ms IS NULL;
END;

CREATE TRIGGER review_preview_sessions_runtime_target_insert
BEFORE INSERT ON review_preview_sessions
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  JOIN runtime_providers provider ON provider.provider_id=target.provider_id
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
    AND provider.bundle_sha256=NEW.bundle_sha256
)
OR NOT EXISTS(
  SELECT 1 FROM import_item_core_validations validation
  WHERE validation.id=NEW.validation_id AND validation.import_item_id=NEW.import_item_id
    AND validation.source_snapshot_id=NEW.source_snapshot_id
    AND validation.provider_id=NEW.provider_id AND validation.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER review_runtime_screenshots_runtime_target_insert
BEFORE INSERT ON review_runtime_screenshots
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER review_uploaded_assets_immutable_delete BEFORE DELETE ON review_uploaded_assets
WHEN NOT EXISTS(SELECT 1 FROM import_items WHERE id=OLD.import_item_id AND payload_state IN ('RELEASING','FAILED'))
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER review_uploaded_assets_immutable_update BEFORE UPDATE ON review_uploaded_assets
BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER rpgmaker_game_profiles_validate_insert
BEFORE INSERT ON rpgmaker_game_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM games game
  WHERE game.id=NEW.game_id AND game.content_kind='RPG_MAKER_PROJECT'
    AND json_extract(game.source_manifest_json,'$.fileCount')=NEW.file_count
    AND json_extract(game.source_manifest_json,'$.totalBytes')=NEW.total_bytes
    AND json_extract(game.source_manifest_json,'$.filesDigest')=NEW.project_fingerprint
)
BEGIN SELECT RAISE(ABORT,'RPG Maker content profile manifest mismatch'); END;

CREATE TRIGGER rpgmaker_game_profiles_validate_update
BEFORE UPDATE ON rpgmaker_game_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM games game
  WHERE game.id=NEW.game_id AND game.content_kind='RPG_MAKER_PROJECT'
    AND json_extract(game.source_manifest_json,'$.fileCount')=NEW.file_count
    AND json_extract(game.source_manifest_json,'$.totalBytes')=NEW.total_bytes
    AND json_extract(game.source_manifest_json,'$.filesDigest')=NEW.project_fingerprint
)
BEGIN SELECT RAISE(ABORT,'RPG Maker content profile manifest mismatch'); END;

CREATE TRIGGER rpgmaker_review_profiles_runtime_target_insert
BEFORE INSERT ON rpgmaker_review_profiles
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER rpgmaker_runtime_validation_checkpoints_format_insert
BEFORE INSERT ON rpgmaker_runtime_validation_checkpoints
WHEN NOT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validations validation
  JOIN runtime_targets target
    ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
  WHERE validation.id=NEW.validation_id AND target.checkpoint_json IS NOT NULL
    AND EXISTS(
      SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
      WHERE readable.type='text' AND readable.value=NEW.checkpoint_format
    )
)
BEGIN SELECT RAISE(ABORT,'invalid runtime checkpoint format'); END;

CREATE TRIGGER rpgmaker_runtime_validation_checkpoints_guarded_delete
BEFORE DELETE ON rpgmaker_runtime_validation_checkpoints
WHEN NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validations validation
  WHERE validation.id=OLD.validation_id AND validation.state IN ('PASSED','FAILED','EXPIRED'))
BEGIN SELECT RAISE(ABORT,'active runtime validation checkpoint is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validation_checkpoints_immutable_update
BEFORE UPDATE ON rpgmaker_runtime_validation_checkpoints
BEGIN SELECT RAISE(ABORT,'runtime validation checkpoint is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validation_gate_events_immutable_delete
BEFORE DELETE ON rpgmaker_runtime_validation_gate_events
BEGIN SELECT RAISE(ABORT,'runtime validation gate evidence is immutable'); END;

CREATE TRIGGER rpgmaker_runtime_validation_gate_events_immutable_update
BEFORE UPDATE ON rpgmaker_runtime_validation_gate_events
BEGIN SELECT RAISE(ABORT,'runtime validation gate evidence is immutable'); END;

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

CREATE TRIGGER rpgmaker_runtime_validations_gate_sequence_update
BEFORE UPDATE OF last_gate_sequence ON rpgmaker_runtime_validations
WHEN NEW.last_gate_sequence<>OLD.last_gate_sequence AND (
  NEW.last_gate_sequence<>OLD.last_gate_sequence+1
  OR NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_gate_events event
    WHERE event.validation_id=NEW.id AND event.sequence=NEW.last_gate_sequence)
)
BEGIN SELECT RAISE(ABORT,'runtime validation gate sequence mismatch'); END;

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

CREATE TRIGGER rpgmaker_runtime_validations_runtime_target_insert
BEFORE INSERT ON rpgmaker_runtime_validations
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

CREATE TRIGGER rpgmaker_runtime_validations_terminal_immutable
BEFORE UPDATE ON rpgmaker_runtime_validations
WHEN OLD.state IN ('PASSED','FAILED','EXPIRED')
BEGIN SELECT RAISE(ABORT,'terminal runtime validation is immutable'); END;

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

CREATE TRIGGER runtime_asset_pack_definitions_immutable_delete
BEFORE DELETE ON runtime_asset_pack_definitions
BEGIN SELECT RAISE(ABORT,'runtime pack definition is immutable'); END;

CREATE TRIGGER runtime_asset_pack_definitions_immutable_update
BEFORE UPDATE ON runtime_asset_pack_definitions
BEGIN SELECT RAISE(ABORT,'runtime pack definition is immutable'); END;

CREATE TRIGGER runtime_asset_pack_files_guarded_delete
BEFORE DELETE ON runtime_asset_pack_files
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_asset_pack_installations installation
  WHERE installation.id=OLD.installation_id AND installation.status='DELETE_PENDING'
)
BEGIN SELECT RAISE(ABORT,'runtime pack file is immutable'); END;

CREATE TRIGGER runtime_asset_pack_files_immutable_update
BEFORE UPDATE ON runtime_asset_pack_files
BEGIN SELECT RAISE(ABORT,'runtime pack file is immutable'); END;

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

CREATE TRIGGER runtime_asset_pack_installations_blob_insert
BEFORE INSERT ON runtime_asset_pack_installations
WHEN NEW.bundle_blob_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM blobs blob WHERE blob.id=NEW.bundle_blob_id AND blob.sha256=NEW.bundle_sha256
)
BEGIN SELECT RAISE(ABORT,'runtime pack bundle blob mismatch'); END;

CREATE TRIGGER runtime_asset_pack_installations_diagnostic_update
BEFORE UPDATE OF diagnostic_json ON runtime_asset_pack_installations
WHEN OLD.status<>'VALIDATING'
BEGIN SELECT RAISE(ABORT,'terminal runtime pack diagnostic is immutable'); END;

CREATE TRIGGER runtime_asset_pack_installations_identity_update
BEFORE UPDATE OF definition_id,files_digest,file_count,total_bytes,source_note,created_by_user_id,created_at_ms
ON runtime_asset_pack_installations
BEGIN SELECT RAISE(ABORT,'runtime pack installation identity is immutable'); END;

CREATE TRIGGER runtime_asset_pack_installations_immutable_delete
BEFORE DELETE ON runtime_asset_pack_installations
BEGIN SELECT RAISE(ABORT,'runtime pack installation is retained for audit'); END;

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
  SELECT 1 FROM game_variant_runtime_packs reference
  WHERE reference.installation_id=OLD.id
)
OR NEW.bundle_blob_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM blobs blob WHERE blob.id=NEW.bundle_blob_id AND blob.sha256=NEW.bundle_sha256
)
BEGIN SELECT RAISE(ABORT,'invalid runtime pack installation transition'); END;

CREATE TRIGGER runtime_asset_pack_installations_version_update
BEFORE UPDATE ON runtime_asset_pack_installations
WHEN NEW.version<>OLD.version+1
BEGIN SELECT RAISE(ABORT,'runtime pack installation version must increment by one'); END;

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

CREATE TRIGGER save_states_published_insert BEFORE INSERT ON save_states
WHEN NOT EXISTS(SELECT 1 FROM games WHERE id=NEW.game_id AND status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE TRIGGER save_states_runtime_target_immutable
BEFORE UPDATE OF checkpoint_format
ON save_states
BEGIN SELECT RAISE(ABORT,'immutable runtime checkpoint snapshot'); END;

CREATE TRIGGER save_states_runtime_target_insert
BEFORE INSERT ON save_states
WHEN NOT EXISTS(
  SELECT 1 FROM launch_sessions launch
  JOIN runtime_targets target ON target.provider_id=launch.provider_id AND target.target_id=launch.target_id
  WHERE launch.id=NEW.source_launch_session_id AND launch.purpose='PRODUCT'
    AND launch.game_id=NEW.game_id AND launch.profile_id=NEW.profile_id
    AND launch.save_access='NORMAL'
    AND target.checkpoint_json IS NOT NULL
    AND EXISTS(
      SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
      WHERE readable.type='text' AND readable.value=NEW.checkpoint_format
    )
)
BEGIN SELECT RAISE(ABORT,'invalid runtime checkpoint snapshot'); END;

CREATE TRIGGER save_states_source_launch_immutable
BEFORE UPDATE OF source_launch_session_id ON save_states
WHEN OLD.source_launch_session_id IS NOT NEW.source_launch_session_id
BEGIN SELECT RAISE(ABORT, 'save state source launch is immutable'); END;

CREATE TRIGGER scrape_attempts_immutable_delete BEFORE DELETE ON metadata_scrape_query_attempts BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER scrape_attempts_immutable_update BEFORE UPDATE ON metadata_scrape_query_attempts BEGIN SELECT RAISE(ABORT, 'immutable'); END;

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

CREATE TRIGGER scrape_candidates_immutable_delete BEFORE DELETE ON scrape_candidates BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER scrape_candidates_immutable_update BEFORE UPDATE ON scrape_candidates BEGIN SELECT RAISE(ABORT, 'immutable'); END;

CREATE TRIGGER server_bios_import_items_runtime_target_insert
BEFORE INSERT ON server_bios_import_items
WHEN NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=NEW.provider_id AND target.target_id=NEW.target_id
)
BEGIN SELECT RAISE(ABORT,'invalid runtime target snapshot'); END;

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

CREATE TRIGGER upload_consumptions_rpgmaker_purpose_insert
BEFORE INSERT ON upload_consumptions
WHEN NOT EXISTS(
  SELECT 1 FROM upload_sessions upload
  WHERE upload.id=NEW.upload_session_id AND (
    upload.purpose='GENERAL' AND NEW.consumer_type<>'RUNTIME_ASSET_PACK_INSTALLATION'
    OR upload.purpose='RPG_MAKER_PROJECT'
      AND NEW.consumer_type IN ('IMPORT_JOB','GAME_CONTENT_REPLACE_JOB')
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

CREATE TRIGGER upload_sessions_purpose_immutable
BEFORE UPDATE OF purpose ON upload_sessions
BEGIN SELECT RAISE(ABORT,'upload purpose is immutable'); END;

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

CREATE TRIGGER variant_files_published_insert BEFORE INSERT ON variant_files
WHEN NOT EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN games game ON game.id=variant.game_id
  WHERE variant.id=NEW.game_variant_id AND game.status='PUBLISHED'
)
BEGIN SELECT RAISE(ABORT,'game payload owner is not published'); END;

CREATE VIEW save_state_runtime_compatibility AS
SELECT save.id AS save_state_id,
CASE WHEN EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
  WHERE variant.game_id=save.game_id AND variant.status='READY'
    AND target.checkpoint_json IS NOT NULL
    AND EXISTS(
      SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
      WHERE readable.type='text' AND readable.value=save.checkpoint_format
    )
) THEN 'AVAILABLE' ELSE 'INCOMPATIBLE_RUNTIME' END AS status
FROM save_states save;
