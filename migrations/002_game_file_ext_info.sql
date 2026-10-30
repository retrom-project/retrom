ALTER TABLE game_file_tab ADD COLUMN ext_info JSONB NOT NULL DEFAULT '{}'::jsonb;
