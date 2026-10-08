CREATE TABLE user_tab (
 id TEXT PRIMARY KEY, username TEXT NOT NULL, display_name TEXT NOT NULL,
 role TEXT NOT NULL, status TEXT NOT NULL, session_version BIGINT NOT NULL, version BIGINT NOT NULL,
 last_login_at_ms BIGINT, created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL,
 disabled_at_ms BIGINT, deleted_at_ms BIGINT
);
CREATE UNIQUE INDEX user_username_idx ON user_tab(username);
CREATE TABLE user_credential_tab (
 user_id TEXT PRIMARY KEY, password_hash TEXT NOT NULL, password_scheme TEXT NOT NULL,
 password_changed_at_ms BIGINT NOT NULL, created_at_ms BIGINT NOT NULL
);
CREATE TABLE auth_session_tab (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL, token_sha256 BYTEA NOT NULL,
 user_session_version BIGINT NOT NULL, created_at_ms BIGINT NOT NULL, last_seen_at_ms BIGINT NOT NULL,
 idle_expires_at_ms BIGINT NOT NULL, absolute_expires_at_ms BIGINT NOT NULL,
 revoked_at_ms BIGINT, revoked_reason TEXT
);
CREATE UNIQUE INDEX auth_session_token_idx ON auth_session_tab(token_sha256);
CREATE INDEX auth_session_user_idx ON auth_session_tab(user_id);
CREATE INDEX auth_session_expiry_idx ON auth_session_tab(absolute_expires_at_ms, idle_expires_at_ms);
CREATE TABLE account_link_tab (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, invited_role TEXT, target_user_id TEXT,
 created_by_user_id TEXT, expires_at_ms BIGINT NOT NULL, consumed_at_ms BIGINT,
 consumed_by_user_id TEXT, revoked_at_ms BIGINT, revoked_by_kind TEXT, revoked_by_user_id TEXT,
 version BIGINT NOT NULL, created_at_ms BIGINT NOT NULL
);
CREATE TABLE instance_state_tab (
 id TEXT PRIMARY KEY, state TEXT NOT NULL, bootstrap_kind TEXT NOT NULL,
 initial_admin_user_id TEXT, test_default_password_active BOOLEAN NOT NULL, version BIGINT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL, initialized_at_ms BIGINT
);
CREATE TABLE platform_instance_tab (
 id TEXT PRIMARY KEY, platform_id TEXT NOT NULL, name TEXT NOT NULL, slug TEXT NOT NULL,
 description TEXT NOT NULL, default_core_id TEXT NOT NULL, enabled BOOLEAN NOT NULL, version BIGINT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE UNIQUE INDEX platform_instance_slug_idx ON platform_instance_tab(slug);
CREATE INDEX platform_instance_platform_idx ON platform_instance_tab(platform_id);
CREATE TABLE platform_instance_core_tab (
 platform_instance_id TEXT NOT NULL, core_id TEXT NOT NULL, sort_order BIGINT NOT NULL,
 PRIMARY KEY(platform_instance_id, core_id)
);
CREATE TABLE game_tab (
 id TEXT PRIMARY KEY, platform_instance_id TEXT NOT NULL, title TEXT NOT NULL, title_initial TEXT NOT NULL,
 description TEXT NOT NULL, developer TEXT NOT NULL, publisher TEXT NOT NULL, genre TEXT NOT NULL,
 players TEXT NOT NULL, release_year BIGINT, content_hash TEXT NOT NULL, runtime_config_json JSONB NOT NULL,
 source TEXT NOT NULL, status TEXT NOT NULL, version BIGINT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL, deleted_at_ms BIGINT
);
CREATE INDEX game_directory_list_idx ON game_tab(platform_instance_id,status,title_initial,id);
CREATE INDEX game_status_idx ON game_tab(status,updated_at_ms,id);
CREATE INDEX game_content_idx ON game_tab(platform_instance_id,content_hash,status);
CREATE TABLE game_file_tab (
 id TEXT PRIMARY KEY, game_id TEXT NOT NULL, logical_key TEXT NOT NULL, role TEXT NOT NULL,
 storage_key TEXT NOT NULL, size_bytes BIGINT NOT NULL, sha256 TEXT NOT NULL, status TEXT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE UNIQUE INDEX game_file_active_idx ON game_file_tab(game_id,logical_key) WHERE status='active';
CREATE INDEX game_file_game_idx ON game_file_tab(game_id,status);
CREATE INDEX game_file_cleanup_idx ON game_file_tab(status,updated_at_ms,id);
CREATE TABLE game_media_tab (
 id TEXT PRIMARY KEY, game_id TEXT NOT NULL, kind TEXT NOT NULL, ordinal BIGINT NOT NULL,
 storage_key TEXT NOT NULL, media_type TEXT NOT NULL, width_px BIGINT, height_px BIGINT,
 size_bytes BIGINT NOT NULL, sha256 TEXT NOT NULL, status TEXT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE UNIQUE INDEX game_media_active_idx ON game_media_tab(game_id,kind,ordinal) WHERE status='active';
CREATE INDEX game_media_game_idx ON game_media_tab(game_id,status);
CREATE INDEX game_media_cleanup_idx ON game_media_tab(status,updated_at_ms,id);
CREATE TABLE save_tab (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL, game_id TEXT NOT NULL, save_type TEXT NOT NULL,
 name TEXT NOT NULL, last_commit_id TEXT NOT NULL, slot_key TEXT, storage_key TEXT NOT NULL, payload_hash TEXT NOT NULL,
 payload_size_bytes BIGINT NOT NULL, screenshot_key TEXT, extinfo JSONB NOT NULL, status TEXT NOT NULL,
 version BIGINT NOT NULL, created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE INDEX save_user_list_idx ON save_tab(user_id,status,updated_at_ms DESC,id);
CREATE INDEX save_user_game_idx ON save_tab(user_id,game_id);
CREATE INDEX save_game_idx ON save_tab(game_id,status);
CREATE INDEX save_cleanup_idx ON save_tab(status,updated_at_ms,id);
CREATE TABLE recent_game_tab (
 user_id TEXT NOT NULL, game_id TEXT NOT NULL, last_played_at_ms BIGINT NOT NULL,
 PRIMARY KEY(user_id,game_id)
);
CREATE INDEX recent_user_time_idx ON recent_game_tab(user_id,last_played_at_ms DESC,game_id);
CREATE INDEX recent_game_idx ON recent_game_tab(game_id);
CREATE TABLE bios_file_tab (
 id TEXT PRIMARY KEY, requirement_key TEXT NOT NULL, original_filename TEXT NOT NULL,
 storage_key TEXT NOT NULL, size_bytes BIGINT NOT NULL, sha256 TEXT NOT NULL, status TEXT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE UNIQUE INDEX bios_file_active_idx ON bios_file_tab(requirement_key) WHERE status='active';
CREATE INDEX bios_file_cleanup_idx ON bios_file_tab(status,updated_at_ms,id);
CREATE TABLE scan_progress_tab (
 id TEXT PRIMARY KEY, scan_type TEXT NOT NULL, status TEXT NOT NULL, total_count BIGINT,
 processed_count BIGINT NOT NULL, imported_count BIGINT NOT NULL, skipped_count BIGINT NOT NULL,
 failed_count BIGINT NOT NULL, error_summary TEXT, created_by_user_id TEXT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL, finished_at_ms BIGINT
);
CREATE INDEX scan_progress_status_idx ON scan_progress_tab(status,updated_at_ms,id);
CREATE TABLE tag_tab (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL, status TEXT NOT NULL, version BIGINT NOT NULL,
 created_by_user_id TEXT NOT NULL, updated_by_user_id TEXT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE UNIQUE INDEX tag_active_name_idx ON tag_tab(name_key) WHERE status='active';
CREATE TABLE game_tag_tab (
 game_id TEXT NOT NULL, tag_id TEXT NOT NULL, assigned_by_user_id TEXT NOT NULL, created_at_ms BIGINT NOT NULL,
 PRIMARY KEY(game_id,tag_id)
);
CREATE INDEX game_tag_tag_idx ON game_tag_tab(tag_id,game_id);
CREATE TABLE favorite_tab (
 user_id TEXT NOT NULL, game_id TEXT NOT NULL, created_at_ms BIGINT NOT NULL, PRIMARY KEY(user_id,game_id)
);
CREATE INDEX favorite_game_idx ON favorite_tab(game_id);
CREATE INDEX favorite_user_time_idx ON favorite_tab(user_id,created_at_ms,game_id);
CREATE TABLE favorite_folder_tab (
 id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, name_key TEXT NOT NULL, version BIGINT NOT NULL,
 created_at_ms BIGINT NOT NULL, updated_at_ms BIGINT NOT NULL
);
CREATE UNIQUE INDEX favorite_folder_name_idx ON favorite_folder_tab(user_id,name_key);
CREATE INDEX favorite_folder_user_idx ON favorite_folder_tab(user_id,created_at_ms,id);
CREATE TABLE favorite_folder_game_tab (
 user_id TEXT NOT NULL, folder_id TEXT NOT NULL, game_id TEXT NOT NULL, created_at_ms BIGINT NOT NULL,
 PRIMARY KEY(user_id,folder_id,game_id)
);
CREATE INDEX favorite_folder_member_idx ON favorite_folder_game_tab(user_id,game_id);
CREATE INDEX favorite_folder_game_idx ON favorite_folder_game_tab(game_id);
