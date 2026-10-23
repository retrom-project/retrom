-- One-time database rebase for upgrading to v0.0.146 or later from v0.0.145.
-- v0.0.146 merges migrations 001-005 into a single 001_schema baseline without partial indexes.
-- Requires a database last started by v0.0.145 (ledger 001_schema … 005_title_order); otherwise it aborts unchanged.
-- Run once with retrom and retrom-web stopped, after a verified backup, before starting the new images.
-- Result: the ledger holds only 001_schema and the schema equals a new v0.0.146 database.
\set ON_ERROR_STOP on
BEGIN;
LOCK TABLE schema_migrations_tab IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF (SELECT array_agg(id ORDER BY id) FROM schema_migrations_tab) IS DISTINCT FROM
     ARRAY['001_schema','002_query_indexes','003_content_packages','004_file_ownership_indexes','005_title_order'] THEN
    RAISE EXCEPTION 'unexpected migration ledger: %', (SELECT array_agg(id ORDER BY id) FROM schema_migrations_tab);
  END IF;
END $$;

-- 001: one active row per key through nullable slot columns instead of partial unique indexes.
ALTER TABLE game_file_tab ADD COLUMN active_slot SMALLINT;
ALTER TABLE game_media_tab ADD COLUMN active_slot SMALLINT;
ALTER TABLE bios_file_tab ADD COLUMN active_slot SMALLINT;
ALTER TABLE tag_tab ADD COLUMN active_slot SMALLINT;
UPDATE game_file_tab SET active_slot=1 WHERE status='active';
UPDATE game_media_tab SET active_slot=1 WHERE status='active';
UPDATE bios_file_tab SET active_slot=1 WHERE status='active';
UPDATE tag_tab SET active_slot=1 WHERE status='active';
DROP INDEX game_file_active_idx;
DROP INDEX game_media_active_idx;
DROP INDEX bios_file_active_idx;
DROP INDEX tag_active_name_idx;
CREATE UNIQUE INDEX game_file_active_idx ON game_file_tab(game_id,logical_key,active_slot);
CREATE UNIQUE INDEX game_media_active_idx ON game_media_tab(game_id,kind,ordinal,active_slot);
CREATE UNIQUE INDEX bios_file_active_idx ON bios_file_tab(requirement_key,active_slot);
CREATE UNIQUE INDEX tag_active_name_idx ON tag_tab(name_key,active_slot);

-- 002 (and the retired 004): plain reference indexes over every status.
DROP INDEX game_file_reference_idx;
DROP INDEX game_media_reference_idx;
DROP INDEX bios_file_reference_idx;
DROP INDEX save_payload_reference_idx;
DROP INDEX save_screenshot_reference_idx;
CREATE INDEX game_file_reference_idx ON game_file_tab(storage_key,status);
CREATE INDEX game_media_reference_idx ON game_media_tab(storage_key,status);
CREATE INDEX bios_file_reference_idx ON bios_file_tab(storage_key,status);
CREATE INDEX save_payload_reference_idx ON save_tab(storage_key,status);
CREATE INDEX save_screenshot_reference_idx ON save_tab(screenshot_key,status);

-- 003: one active project package per game.
ALTER TABLE game_file_tab ADD COLUMN package_slot SMALLINT;
UPDATE game_file_tab SET package_slot=1 WHERE status='active' AND role='project_package';
DROP INDEX game_file_project_package_idx;
CREATE UNIQUE INDEX game_file_project_package_idx ON game_file_tab(game_id,package_slot);

-- 005: the baseline declares title order columns without defaults; every write sets them.
ALTER TABLE game_tab ALTER COLUMN title_sort_name DROP DEFAULT;
ALTER TABLE game_tab ALTER COLUMN title_sort_key DROP DEFAULT;
ALTER TABLE game_tab ALTER COLUMN title_order_version DROP DEFAULT;

-- Ledger: everything above is the merged 001_schema baseline.
DELETE FROM schema_migrations_tab;
INSERT INTO schema_migrations_tab(id) VALUES('001_schema');

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_index x JOIN pg_class c ON c.oid=x.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace
             WHERE n.nspname='public' AND x.indpred IS NOT NULL) THEN
    RAISE EXCEPTION 'partial index remains';
  END IF;
END $$;
COMMIT;
