BEGIN;
PRAGMA defer_foreign_keys = ON;

INSERT INTO blobs(id, sha256, size_bytes, md5, sha1, crc32, media_type, created_at_ms) VALUES
  ('base-blob',    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 1024, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'aaaaaaaa', 'application/zip', 1),
  ('derived-blob', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 1030, 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'bbbbbbbb', 'application/zip', 1);

INSERT INTO core_artifacts(id, core_id, emulatorjs_version, bundle_version, flavor, relative_path, size_bytes, sha256, source_commit, provenance_json, compatibility_config_json, enabled, created_at_ms, updated_at_ms)
VALUES('dos-artifact', 'dosbox_pure', '4.2.3', 'fixture', 'THREAD_WASM', 'data/cores/dosbox_pure-thread-wasm.data', 1, 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc', NULL, '{}', '{"requestedArtifactBasename":"dosbox_pure-thread-wasm.data"}', 1, 1, 1);

INSERT INTO game_metadata_revisions(id, game_id, title, description, developer, publisher, genre, players, release_year, source_kind, source_ref_id, created_at_ms)
VALUES('dos-metadata', 'dos-game', 'Fixture', '', '', '', '', NULL, NULL, 'ADMIN_EDIT', NULL, 1);
INSERT INTO game_content_revisions(id, game_id, source_kind, source_ref_id, source_manifest_json, source_manifest_digest, created_at_ms)
VALUES('dos-content', 'dos-game', 'ADMIN_REPLACE', 'fixture', '{}', 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd', 1);
INSERT INTO games(id, platform_instance_id, status, current_metadata_revision_id, current_content_revision_id, search_text, created_at_ms, updated_at_ms)
VALUES('dos-game', '01980000-0000-7000-8000-000000000009', 'PUBLISHED', 'dos-metadata', 'dos-content', 'fixture', 1, 1);
INSERT INTO game_variant_revisions(id, game_variant_id, game_content_revision_id, core_artifact_id, dat_version_id, validation_input_digest, emulator_game_id, status, compatibility_code, dependency_snapshot_json, default_dos_entry, created_at_ms)
VALUES('dos-revision', 'dos-variant', 'dos-content', 'dos-artifact', NULL, 'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee', 1, 'READY', 'READY', '{}', 'DOOM/DOOM.EXE', 1);
INSERT INTO game_variants(id, game_id, core_id, current_revision_id, created_at_ms, updated_at_ms)
VALUES('dos-variant', 'dos-game', 'dosbox_pure', 'dos-revision', 1, 1);
INSERT INTO variant_files(game_variant_revision_id, role, logical_name, blob_id, sort_order)
VALUES('dos-revision', 'DOS_LAUNCH_BUNDLE', 'game.zip', 'base-blob', 0);

INSERT INTO launch_sessions(id, profile_id, game_id, game_variant_revision_id, core_artifact_id, save_state_id, dos_entry_path, persistent_save_base_revision_id, return_to, credential_sha256, state, bootstrap_expires_at_ms, hard_expires_at_ms, created_at_ms, updated_at_ms)
VALUES('dos-launch', 'local', 'dos-game', 'dos-revision', 'dos-artifact', NULL, 'DOOM/DOOM.EXE', NULL, '/', zeroblob(32), 'CREATED', 1000, 2000, 1, 1);
INSERT INTO launch_content_files(launch_session_id, logical_name, blob_id, format_version, created_at_ms)
VALUES('dos-launch', 'game-legacy.zip', 'derived-blob', 'RETROM_DOS_DIRECT_ZIP_V1', 1);

COMMIT;
