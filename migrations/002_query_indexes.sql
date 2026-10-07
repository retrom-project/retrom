DROP INDEX IF EXISTS game_directory_list_idx;
CREATE INDEX game_directory_list_idx ON game_tab(platform_instance_id,status,title_initial,title,id);
CREATE INDEX IF NOT EXISTS game_list_idx ON game_tab(status,title_initial,title,id);
CREATE INDEX IF NOT EXISTS game_file_reference_idx ON game_file_tab(storage_key) WHERE status='active';
CREATE INDEX IF NOT EXISTS game_media_reference_idx ON game_media_tab(storage_key) WHERE status='active';
CREATE INDEX IF NOT EXISTS bios_file_reference_idx ON bios_file_tab(storage_key) WHERE status='active';
CREATE INDEX IF NOT EXISTS save_payload_reference_idx ON save_tab(storage_key) WHERE status='active';
CREATE INDEX IF NOT EXISTS save_screenshot_reference_idx ON save_tab(screenshot_key)
 WHERE status='active' AND screenshot_key IS NOT NULL;
