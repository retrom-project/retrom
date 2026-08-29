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
  title_initial TEXT NOT NULL CHECK(
    length(title_initial)=1 AND (title_initial='#' OR title_initial GLOB '[0-9]' OR title_initial GLOB '[A-Z]')
  ),
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
  content_kind TEXT NOT NULL DEFAULT 'SINGLE_FILE' CHECK(content_kind IN (
    'SINGLE_FILE','DOS_BUNDLE','MULTI_DISC_M3U_V1','RPG_MAKER_PROJECT_V1','ONS_PROJECT_V1','KIRIKIRI_PROJECT_V1'
  )),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('IMPORT_REVIEW','ADMIN_REPLACE','SERVER_PEGASUS_IMPORT','SERVER_EMULATIONSTATION_IMPORT')),
  source_ref_id TEXT NOT NULL,
  source_manifest_json TEXT NOT NULL,
  source_manifest_digest TEXT NOT NULL CHECK(length(source_manifest_digest)=64),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(id,game_id),
  CHECK(content_kind<>'RPG_MAKER_PROJECT_V1' OR
    json_valid(source_manifest_json)
    AND json_extract(source_manifest_json,'$.schemaVersion')=2
    AND json_extract(source_manifest_json,'$.contentKind')='RPG_MAKER_PROJECT_V1'
    AND json_type(source_manifest_json,'$.fileCount')='integer'
    AND json_extract(source_manifest_json,'$.fileCount') BETWEEN 1 AND 10000
    AND json_type(source_manifest_json,'$.totalBytes')='integer'
    AND json_extract(source_manifest_json,'$.totalBytes') BETWEEN 0 AND 34359738368
    AND length(json_extract(source_manifest_json,'$.filesDigest'))=64
    AND json_extract(source_manifest_json,'$.filesDigest')=lower(json_extract(source_manifest_json,'$.filesDigest'))
  ),
  CHECK(content_kind<>'ONS_PROJECT_V1' OR
    json_valid(source_manifest_json)
    AND json_extract(source_manifest_json,'$.schemaVersion')=2
    AND json_extract(source_manifest_json,'$.contentKind')='ONS_PROJECT_V1'
    AND json_type(source_manifest_json,'$.fileCount')='integer'
    AND json_extract(source_manifest_json,'$.fileCount') BETWEEN 1 AND 10000
    AND json_type(source_manifest_json,'$.totalBytes')='integer'
    AND json_extract(source_manifest_json,'$.totalBytes') BETWEEN 0 AND 34359738368
    AND length(json_extract(source_manifest_json,'$.filesDigest'))=64
    AND json_extract(source_manifest_json,'$.filesDigest')=lower(json_extract(source_manifest_json,'$.filesDigest'))
  ),
  CHECK(content_kind<>'KIRIKIRI_PROJECT_V1' OR
    json_valid(source_manifest_json)
    AND json_extract(source_manifest_json,'$.schemaVersion')=2
    AND json_extract(source_manifest_json,'$.contentKind')='KIRIKIRI_PROJECT_V1'
    AND json_type(source_manifest_json,'$.fileCount')='integer'
    AND json_extract(source_manifest_json,'$.fileCount') BETWEEN 1 AND 10000
    AND json_type(source_manifest_json,'$.totalBytes')='integer'
    AND json_extract(source_manifest_json,'$.totalBytes') BETWEEN 0 AND 34359738368
    AND length(json_extract(source_manifest_json,'$.filesDigest'))=64
    AND json_extract(source_manifest_json,'$.filesDigest')=lower(json_extract(source_manifest_json,'$.filesDigest'))
  )
);

CREATE TABLE "game_content_files" (
  game_content_revision_id TEXT NOT NULL REFERENCES game_content_revisions(id),
  role TEXT NOT NULL CHECK(role IN (
    'CONTENT','DOS_SOURCE','COMPANION','PLAYLIST_SOURCE','DISC','PROJECT_FILE',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
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
  route_key TEXT NOT NULL CHECK(length(route_key) BETWEEN 1 AND 160),
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
  CHECK(status='READY' OR emulator_game_id IS NULL)
);

CREATE TABLE rpgmaker_content_profiles (
  content_revision_id TEXT PRIMARY KEY REFERENCES game_content_revisions(id),
  evidence_family TEXT NOT NULL CHECK(evidence_family IN ('RPG2K','RGSS','MV','MZ')),
  evidence_generation TEXT CHECK(evidence_generation IS NULL OR evidence_generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  evidence_confidence TEXT NOT NULL CHECK(evidence_confidence IN ('MATCHED','FAMILY_ONLY')),
  engine_version TEXT,
  entry_html_path TEXT,
  file_count INTEGER NOT NULL CHECK(file_count BETWEEN 1 AND 10000),
  total_bytes INTEGER NOT NULL CHECK(total_bytes BETWEEN 0 AND 34359738368),
  project_fingerprint TEXT NOT NULL CHECK(length(project_fingerprint)=64 AND project_fingerprint=lower(project_fingerprint)),
  requirements_sha256 TEXT NOT NULL CHECK(length(requirements_sha256)=64 AND requirements_sha256=lower(requirements_sha256)),
  analysis_json TEXT NOT NULL CHECK(json_valid(analysis_json) AND length(CAST(analysis_json AS BLOB))<=262144),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  CHECK(
    evidence_confidence='FAMILY_ONLY' AND evidence_family='RPG2K' AND evidence_generation IS NULL
    OR evidence_confidence='MATCHED' AND evidence_generation IS NOT NULL
  ),
  CHECK(
    evidence_family='RPG2K' AND (evidence_generation IS NULL OR evidence_generation IN ('RPG2000','RPG2003'))
    OR evidence_family='RGSS' AND evidence_generation IN ('RPGXP','RPGVX','RPGVXACE')
    OR evidence_family='MV' AND evidence_generation='RPGMV'
    OR evidence_family='MZ' AND evidence_generation='RPGMZ'
  ),
  CHECK(
    evidence_family IN ('MV','MZ') AND entry_html_path='index.html'
    OR evidence_family NOT IN ('MV','MZ') AND entry_html_path IS NULL
  )
);

CREATE TABLE rpgmaker_variant_profiles (
  game_variant_revision_id TEXT PRIMARY KEY REFERENCES game_variant_revisions(id),
  generation TEXT NOT NULL CHECK(generation IN (
    'RPG2000','RPG2003','RPGXP','RPGVX','RPGVXACE','RPGMV','RPGMZ'
  )),
  route_key TEXT NOT NULL,
  adapter_id TEXT NOT NULL,
  adapter_abi TEXT NOT NULL,
  artifact_set_sha256 TEXT NOT NULL CHECK(length(artifact_set_sha256)=64 AND artifact_set_sha256=lower(artifact_set_sha256)),
  dependency_snapshot_sha256 TEXT NOT NULL CHECK(
    length(dependency_snapshot_sha256)=64 AND dependency_snapshot_sha256=lower(dependency_snapshot_sha256)
  ),
  runtime_validation_id TEXT UNIQUE REFERENCES rpgmaker_runtime_validations(id)
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
  role TEXT NOT NULL CHECK(role IN (
    'PARENT','BIOS_BUNDLE','DOS_LAUNCH_BUNDLE','MULTI_DISC_PLAYLIST',
    'RPG_EASYRPG_INDEX','RPG_MAKER_LAUNCH_BUNDLE'
  )),
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
