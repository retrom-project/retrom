CREATE TABLE IF NOT EXISTS game_file_member_tab (
 game_file_id TEXT NOT NULL,
 logical_path TEXT COLLATE "C" NOT NULL,
 data_offset BIGINT NOT NULL,
 size_bytes BIGINT NOT NULL,
 sha256 TEXT NOT NULL,
 media_type TEXT NOT NULL,
 PRIMARY KEY(game_file_id,logical_path)
);
ALTER TABLE game_file_tab ADD COLUMN IF NOT EXISTS package_slot SMALLINT;
CREATE UNIQUE INDEX IF NOT EXISTS game_file_project_package_idx ON game_file_tab(game_id,package_slot);
