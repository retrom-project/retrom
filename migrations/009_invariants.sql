CREATE INDEX fk_platform_instances_default_core ON platform_instances(default_core_id);
CREATE INDEX fk_core_artifacts_core ON core_artifacts(core_id);
CREATE INDEX fk_upload_files_session ON upload_files(upload_session_id);
CREATE INDEX fk_archive_entries_materialized ON archive_entries(materialized_blob_id);
CREATE INDEX fk_bios_installations_blob ON bios_installations(blob_id);
CREATE INDEX fk_dat_versions_blob ON dat_versions(blob_id);
CREATE INDEX fk_import_jobs_platform ON import_jobs(target_platform_instance_id);
CREATE INDEX fk_import_items_job ON import_items(import_job_id);
CREATE INDEX fk_game_metadata_game ON game_metadata_revisions(game_id);
CREATE INDEX fk_game_content_game ON game_content_revisions(game_id);
CREATE INDEX fk_variant_revision_content ON game_variant_revisions(game_content_revision_id);
CREATE INDEX fk_launch_game ON launch_sessions(game_id);

CREATE TRIGGER games_current_metadata_owner_insert
BEFORE INSERT ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_metadata_revisions WHERE id = NEW.current_metadata_revision_id AND game_id = NEW.id
  ) THEN RAISE(ABORT, 'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_content_revisions WHERE id = NEW.current_content_revision_id AND game_id = NEW.id
  ) THEN RAISE(ABORT, 'current content owner mismatch') END;
END;

CREATE TRIGGER games_current_owner_update
BEFORE UPDATE OF current_metadata_revision_id, current_content_revision_id ON games
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_metadata_revisions WHERE id = NEW.current_metadata_revision_id AND game_id = NEW.id
  ) THEN RAISE(ABORT, 'current metadata owner mismatch') END;
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_content_revisions WHERE id = NEW.current_content_revision_id AND game_id = NEW.id
  ) THEN RAISE(ABORT, 'current content owner mismatch') END;
END;

CREATE TRIGGER game_variants_current_ready_insert
BEFORE INSERT ON game_variants WHEN NEW.current_revision_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_variant_revisions WHERE id = NEW.current_revision_id AND game_variant_id = NEW.id AND status = 'READY'
  ) THEN RAISE(ABORT, 'variant current must be ready and owned') END;
END;

CREATE TRIGGER game_variants_current_ready_update
BEFORE UPDATE OF current_revision_id ON game_variants WHEN NEW.current_revision_id IS NOT NULL
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM game_variant_revisions WHERE id = NEW.current_revision_id AND game_variant_id = NEW.id AND status = 'READY'
  ) THEN RAISE(ABORT, 'variant current must be ready and owned') END;
END;

CREATE TRIGGER platform_cores_in_use_disable
BEFORE UPDATE OF enabled ON platform_cores WHEN NEW.enabled = 0
BEGIN
  SELECT CASE WHEN EXISTS (
    SELECT 1 FROM platform_instances WHERE platform_id = OLD.platform_id AND default_core_id = OLD.core_id AND deleted_at_ms IS NULL
  ) THEN RAISE(ABORT, 'platform core is in use') END;
END;

CREATE TRIGGER metadata_provider_cache_owner_insert
BEFORE INSERT ON metadata_provider_cache
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM metadata_provider_responses
    WHERE id = NEW.current_response_id AND provider = NEW.provider AND request_digest = NEW.request_digest
  ) THEN RAISE(ABORT, 'provider cache response mismatch') END;
END;

CREATE TRIGGER persistent_saves_current_owner_update
BEFORE UPDATE OF current_revision_id ON persistent_saves
BEGIN
  SELECT CASE WHEN NOT EXISTS (
    SELECT 1 FROM persistent_save_revisions WHERE id = NEW.current_revision_id AND persistent_save_id = NEW.id
  ) THEN RAISE(ABORT, 'persistent save current owner mismatch') END;
END;
