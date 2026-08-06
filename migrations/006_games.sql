CREATE TABLE games (
  id TEXT PRIMARY KEY,
  platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  status TEXT NOT NULL CHECK(status IN ('PUBLISHED','DELETED')),
  current_metadata_revision_id TEXT NOT NULL,
  current_content_revision_id TEXT NOT NULL,
  search_text TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  deleted_at_ms INTEGER,
  FOREIGN KEY(current_metadata_revision_id) REFERENCES game_metadata_revisions(id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(current_content_revision_id) REFERENCES game_content_revisions(id) DEFERRABLE INITIALLY DEFERRED,
  CHECK((status = 'DELETED') = (deleted_at_ms IS NOT NULL))
);

CREATE TABLE game_metadata_revisions (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  title TEXT NOT NULL CHECK(length(title) > 0),
  description TEXT NOT NULL,
  developer TEXT NOT NULL,
  publisher TEXT NOT NULL,
  genre TEXT NOT NULL,
  players INTEGER CHECK(players IS NULL OR players BETWEEN 1 AND 64),
  release_year INTEGER,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_EDIT','RESCRAPE_APPLY')),
  source_ref_id TEXT,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(id, game_id),
  CHECK((source_kind = 'ADMIN_EDIT') = (source_ref_id IS NULL))
);

CREATE TABLE game_assets (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  metadata_revision_id TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  kind TEXT NOT NULL CHECK(kind IN ('COVER','BACKGROUND','SCREENSHOT')),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  width_px INTEGER NOT NULL CHECK(width_px > 0),
  height_px INTEGER NOT NULL CHECK(height_px > 0),
  media_type TEXT NOT NULL CHECK(media_type IN ('image/png','image/jpeg','image/webp')),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(metadata_revision_id, kind, ordinal),
  FOREIGN KEY(metadata_revision_id, game_id) REFERENCES game_metadata_revisions(id, game_id)
);

CREATE TABLE game_content_revisions (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_REPLACE')),
  source_ref_id TEXT NOT NULL,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest) = 64),
  created_at_ms INTEGER NOT NULL,
  UNIQUE(id, game_id)
);

CREATE TABLE game_content_files (
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL,
  PRIMARY KEY(game_content_revision_id, role, logical_name),
  FOREIGN KEY(source_archive_blob_id, source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id, ordinal),
  CHECK((source_archive_blob_id IS NULL) = (source_archive_entry_ordinal IS NULL))
);

CREATE TABLE game_variants (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  current_revision_id TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(game_id, core_id),
  FOREIGN KEY(current_revision_id) REFERENCES game_variant_revisions(id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE game_variant_revisions (
  id TEXT PRIMARY KEY,
  game_variant_id TEXT NOT NULL REFERENCES game_variants(id),
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  core_artifact_id TEXT NOT NULL REFERENCES core_artifacts(id),
  dat_version_id TEXT REFERENCES dat_versions(id),
  validation_input_digest TEXT NOT NULL CHECK(length(validation_input_digest) = 64),
  emulator_game_id INTEGER UNIQUE CHECK(emulator_game_id IS NULL OR emulator_game_id BETWEEN 1 AND 9007199254740991),
  status TEXT NOT NULL CHECK(status IN ('READY','BLOCKED','INCOMPATIBLE')),
  compatibility_code TEXT NOT NULL,
  dependency_snapshot_json TEXT NOT NULL,
  default_dos_entry TEXT,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(game_variant_id, validation_input_digest),
  UNIQUE(id, game_variant_id),
  CHECK((status = 'READY') = (emulator_game_id IS NOT NULL))
);

CREATE TABLE variant_files (
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL,
  PRIMARY KEY(game_variant_revision_id, role, logical_name)
);

CREATE TABLE dos_entries (
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  normalized_path TEXT NOT NULL,
  original_relative_path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('EXE','COM','BAT')),
  rank INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  direct_launch_safe INTEGER NOT NULL CHECK(direct_launch_safe IN (0,1)),
  PRIMARY KEY(game_content_revision_id, normalized_path)
);

CREATE TABLE variant_dependencies (
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  kind TEXT NOT NULL CHECK(kind IN ('PARENT','BIOS_OR_BASE')),
  logical_archive TEXT NOT NULL,
  dat_version_id TEXT NOT NULL REFERENCES dat_versions(id),
  source_machine_name TEXT NOT NULL,
  required_entries_json TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('SATISFIED_BY_CONTENT','SATISFIED_EXTERNAL','HASH_WARNING','MISSING','MISMATCH','UNSUPPORTED')),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(game_variant_revision_id, kind, logical_archive),
  CHECK(kind = 'BIOS_OR_BASE' OR state != 'HASH_WARNING')
);

CREATE INDEX games_library ON games(status, platform_instance_id, search_text, id);
CREATE INDEX game_variants_game ON game_variants(game_id, core_id);

CREATE TRIGGER game_metadata_revisions_immutable_update BEFORE UPDATE ON game_metadata_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_metadata_revisions_immutable_delete BEFORE DELETE ON game_metadata_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_assets_immutable_update BEFORE UPDATE ON game_assets BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_assets_immutable_delete BEFORE DELETE ON game_assets BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_content_revisions_immutable_update BEFORE UPDATE ON game_content_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_content_revisions_immutable_delete BEFORE DELETE ON game_content_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_content_files_immutable_update BEFORE UPDATE ON game_content_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_content_files_immutable_delete BEFORE DELETE ON game_content_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_variant_revisions_immutable_update BEFORE UPDATE ON game_variant_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER game_variant_revisions_immutable_delete BEFORE DELETE ON game_variant_revisions BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER variant_files_immutable_update BEFORE UPDATE ON variant_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER variant_files_immutable_delete BEFORE DELETE ON variant_files BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER dos_entries_immutable_update BEFORE UPDATE ON dos_entries BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER dos_entries_immutable_delete BEFORE DELETE ON dos_entries BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER variant_dependencies_immutable_update BEFORE UPDATE ON variant_dependencies BEGIN SELECT RAISE(ABORT, 'immutable'); END;
CREATE TRIGGER variant_dependencies_immutable_delete BEFORE DELETE ON variant_dependencies BEGIN SELECT RAISE(ABORT, 'immutable'); END;
