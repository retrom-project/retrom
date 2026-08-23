-- Clean pre-release baseline: library.

CREATE TABLE games (
  id TEXT PRIMARY KEY,
  platform_instance_id TEXT NOT NULL REFERENCES platform_instances(id),
  status TEXT NOT NULL CHECK(status IN ('PUBLISHED','DELETED')),
  payload_state TEXT NOT NULL DEFAULT 'RETAINED' CHECK(payload_state IN ('RETAINED','RELEASING','RELEASED','FAILED')),
  payload_release_job_id TEXT UNIQUE REFERENCES jobs(id),
  payload_released_at_ms INTEGER,
  payload_last_error_code TEXT,
  current_metadata_revision_id TEXT NOT NULL,
  current_content_revision_id TEXT NOT NULL,
  search_text TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  deleted_at_ms INTEGER,
  FOREIGN KEY(current_metadata_revision_id) REFERENCES game_metadata_revisions(id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(current_content_revision_id) REFERENCES game_content_revisions(id) DEFERRABLE INITIALLY DEFERRED,
  CHECK((status = 'DELETED') = (deleted_at_ms IS NOT NULL)),
  CHECK(status<>'PUBLISHED' OR payload_state='RETAINED'),
  CHECK(status<>'DELETED' OR payload_state IN ('RELEASING','RELEASED','FAILED')),
  CHECK(
    payload_state='RETAINED' AND payload_release_job_id IS NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASING' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NULL OR
    payload_state='RELEASED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NOT NULL AND payload_last_error_code IS NULL OR
    payload_state='FAILED' AND payload_release_job_id IS NOT NULL AND payload_released_at_ms IS NULL AND payload_last_error_code IS NOT NULL
  )
);

CREATE TABLE "game_metadata_revisions" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  title TEXT NOT NULL CHECK(length(title)>0),
  description TEXT NOT NULL,
  developer TEXT NOT NULL,
  publisher TEXT NOT NULL,
  genre TEXT NOT NULL,
  players INTEGER CHECK(players IS NULL OR players BETWEEN 1 AND 64),
  release_year INTEGER,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_EDIT','RESCRAPE_APPLY','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_IMPORT')),
  source_ref_id TEXT,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(id,game_id),
  CHECK((source_kind='ADMIN_EDIT')=(source_ref_id IS NULL))
);

CREATE TABLE "game_assets" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  metadata_revision_id TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  kind TEXT NOT NULL CHECK(kind IN ('COVER','BACKGROUND','SCREENSHOT','VIDEO')),
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 31),
  width_px INTEGER,
  height_px INTEGER,
  media_type TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(metadata_revision_id,kind,ordinal),
  FOREIGN KEY(metadata_revision_id,game_id) REFERENCES game_metadata_revisions(id,game_id),
  CHECK(
    (kind IN ('COVER','BACKGROUND','SCREENSHOT') AND width_px>0 AND height_px>0 AND media_type IN ('image/png','image/jpeg','image/webp')) OR
    (kind='VIDEO' AND ordinal=0 AND width_px IS NULL AND height_px IS NULL AND media_type IN ('video/mp4','video/webm'))
  )
);

CREATE TABLE "game_content_revisions" (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' CHECK(content_kind IN ('SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1')),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_REPLACE','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_IMPORT')),
  source_ref_id TEXT NOT NULL,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(id,game_id)
);

CREATE TABLE "game_content_files" (
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  role TEXT NOT NULL CHECK(role IN ('CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  source_archive_blob_id TEXT,
  source_archive_entry_ordinal INTEGER,
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_content_revision_id,role,logical_name),
  FOREIGN KEY(source_archive_blob_id,source_archive_entry_ordinal) REFERENCES archive_entries(archive_blob_id,ordinal),
  CHECK((source_archive_blob_id IS NULL)=(source_archive_entry_ordinal IS NULL))
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

CREATE TABLE "variant_files" (
  game_variant_revision_id TEXT NOT NULL REFERENCES game_variant_revisions(id),
  role TEXT NOT NULL CHECK(role IN ('PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST')),
  logical_name TEXT NOT NULL,
  blob_id TEXT NOT NULL REFERENCES blobs(id),
  sort_order INTEGER NOT NULL CHECK(sort_order>=0),
  PRIMARY KEY(game_variant_revision_id,role,logical_name)
);

CREATE TABLE tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 40 AND length(CAST(name AS BLOB))<=160),
  name_key TEXT NOT NULL CHECK(length(name_key)>=1 AND length(CAST(name_key AS BLOB))<=160),
  search_text TEXT NOT NULL CHECK(length(search_text)>=1 AND length(CAST(search_text AS BLOB))<=160),
  status TEXT NOT NULL CHECK(status IN ('ACTIVE','DELETED')),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_by_user_id TEXT NOT NULL REFERENCES users(id),
  updated_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  deleted_at_ms INTEGER,
  CHECK((status='DELETED')=(deleted_at_ms IS NOT NULL)),
  CHECK(length(id)=36 AND lower(id)=id
    AND id NOT GLOB '*[^0-9a-f-]*'
    AND substr(id,9,1)='-' AND substr(id,14,1)='-'
    AND substr(id,19,1)='-' AND substr(id,24,1)='-')
);

CREATE TABLE game_tags (
  game_id TEXT NOT NULL REFERENCES games(id),
  tag_id TEXT NOT NULL REFERENCES tags(id),
  assigned_by_user_id TEXT NOT NULL REFERENCES users(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(game_id,tag_id)
);

CREATE TABLE favorite_games (
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  game_id TEXT NOT NULL REFERENCES games(id),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(profile_id,game_id),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-'),
  CHECK(length(game_id)=36 AND lower(game_id)=game_id
    AND game_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(game_id,9,1)='-' AND substr(game_id,14,1)='-'
    AND substr(game_id,19,1)='-' AND substr(game_id,24,1)='-')
);

CREATE TABLE favorite_folders (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id),
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 40 AND length(CAST(name AS BLOB))<=160),
  name_key TEXT NOT NULL CHECK(length(name_key)>=1 AND length(CAST(name_key AS BLOB))<=160),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  UNIQUE(profile_id,id),
  UNIQUE(profile_id,name_key),
  CHECK(length(id)=36 AND lower(id)=id
    AND id NOT GLOB '*[^0-9a-f-]*'
    AND substr(id,9,1)='-' AND substr(id,14,1)='-'
    AND substr(id,19,1)='-' AND substr(id,24,1)='-'),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-')
);

CREATE TABLE favorite_folder_games (
  profile_id TEXT NOT NULL,
  folder_id TEXT NOT NULL,
  game_id TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  PRIMARY KEY(profile_id,folder_id,game_id),
  FOREIGN KEY(profile_id,folder_id) REFERENCES favorite_folders(profile_id,id),
  FOREIGN KEY(profile_id,game_id) REFERENCES favorite_games(profile_id,game_id),
  CHECK(length(profile_id)=36 AND lower(profile_id)=profile_id
    AND profile_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(profile_id,9,1)='-' AND substr(profile_id,14,1)='-'
    AND substr(profile_id,19,1)='-' AND substr(profile_id,24,1)='-'),
  CHECK(length(folder_id)=36 AND lower(folder_id)=folder_id
    AND folder_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(folder_id,9,1)='-' AND substr(folder_id,14,1)='-'
    AND substr(folder_id,19,1)='-' AND substr(folder_id,24,1)='-'),
  CHECK(length(game_id)=36 AND lower(game_id)=game_id
    AND game_id NOT GLOB '*[^0-9a-f-]*'
    AND substr(game_id,9,1)='-' AND substr(game_id,14,1)='-'
    AND substr(game_id,19,1)='-' AND substr(game_id,24,1)='-')
);
