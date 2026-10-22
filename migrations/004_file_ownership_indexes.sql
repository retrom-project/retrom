DROP INDEX game_file_reference_idx;
DROP INDEX game_media_reference_idx;
DROP INDEX bios_file_reference_idx;
DROP INDEX save_payload_reference_idx;
DROP INDEX save_screenshot_reference_idx;

CREATE INDEX game_file_reference_idx ON game_file_tab(storage_key) WHERE status IN ('active','deleted');
CREATE INDEX game_media_reference_idx ON game_media_tab(storage_key) WHERE status IN ('active','deleted');
CREATE INDEX bios_file_reference_idx ON bios_file_tab(storage_key) WHERE status IN ('active','deleted');
CREATE INDEX save_payload_reference_idx ON save_tab(storage_key) WHERE status IN ('active','deleted');
CREATE INDEX save_screenshot_reference_idx ON save_tab(screenshot_key)
 WHERE status IN ('active','deleted') AND screenshot_key IS NOT NULL;
