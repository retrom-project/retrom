ALTER TABLE game_tab ADD COLUMN IF NOT EXISTS title_sort_name TEXT NOT NULL DEFAULT '';
ALTER TABLE game_tab ADD COLUMN IF NOT EXISTS title_sort_key TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE game_tab ADD COLUMN IF NOT EXISTS title_order_version BIGINT NOT NULL DEFAULT 0;
DROP INDEX IF EXISTS game_list_idx;
DROP INDEX IF EXISTS game_directory_list_idx;
CREATE INDEX game_list_idx ON game_tab(status,title_sort_key,title,id);
CREATE INDEX game_directory_list_idx ON game_tab(platform_instance_id,status,title_sort_key,title,id);
CREATE INDEX IF NOT EXISTS game_title_order_version_idx ON game_tab(title_order_version);
