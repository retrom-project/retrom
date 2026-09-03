-- Clean pre-release baseline: upload_archive.

CREATE TABLE upload_sessions (
  id TEXT PRIMARY KEY,
  purpose TEXT NOT NULL DEFAULT 'GENERAL' CHECK(purpose IN (
    'GENERAL','RPG_MAKER_PROJECT','ONS_PROJECT','KIRIKIRI_PROJECT','BUTTERSCOTCH_PROJECT',
    'TYRANOSCRIPT_PROJECT','RUNTIME_ASSET_PACK'
  )),
  state TEXT NOT NULL CHECK(state IN ('CREATED','UPLOADING','FINALIZING','COMPLETE','FAILED','CANCELLED','EXPIRED')),
  source_type TEXT NOT NULL CHECK(source_type IN ('FILES','DIRECTORY')),
  total_files INTEGER NOT NULL CHECK(total_files BETWEEN 1 AND 10000),
  total_bytes INTEGER NOT NULL CHECK(total_bytes BETWEEN 0 AND 34359738368),
  manifest_digest TEXT NOT NULL CHECK(length(manifest_digest) = 64),
  finalization_no INTEGER NOT NULL DEFAULT 0 CHECK(finalization_no >= 0),
  finalize_job_id TEXT UNIQUE REFERENCES jobs(id),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  expires_at_ms INTEGER NOT NULL,
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  unconsumed_pruned_at_ms INTEGER,
  last_error_code TEXT
  , CHECK(purpose='GENERAL' OR source_type='DIRECTORY' OR total_files=1)
);

CREATE TABLE upload_files (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  relative_path TEXT NOT NULL,
  declared_size_bytes INTEGER NOT NULL CHECK(declared_size_bytes BETWEEN 0 AND 8589934592),
  received_size_bytes INTEGER NOT NULL DEFAULT 0 CHECK(received_size_bytes >= 0),
  final_blob_id TEXT REFERENCES blobs(id),
  state TEXT NOT NULL CHECK(state IN ('PENDING','PARTIAL','FINALIZING','COMPLETE','FAILED','PURGED')),
  payload_released_at_ms INTEGER,
  last_error_code TEXT,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(upload_session_id, relative_path),
  CHECK(
    state='COMPLETE' AND final_blob_id IS NOT NULL AND payload_released_at_ms IS NULL OR
    state='PURGED' AND final_blob_id IS NULL AND payload_released_at_ms IS NOT NULL OR
    state NOT IN ('COMPLETE','PURGED') AND final_blob_id IS NULL AND payload_released_at_ms IS NULL
  )
);

CREATE TABLE upload_parts (
  upload_file_id TEXT NOT NULL REFERENCES upload_files(id),
  part_no INTEGER NOT NULL CHECK(part_no >= 0),
  offset_bytes INTEGER NOT NULL CHECK(offset_bytes >= 0),
  size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 8388608),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  storage_key TEXT NOT NULL UNIQUE,
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(upload_file_id, part_no)
);

CREATE TABLE archive_entries (
  archive_blob_id TEXT NOT NULL REFERENCES blobs(id),
  ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
  original_relative_path TEXT NOT NULL,
  normalized_path TEXT NOT NULL,
  ascii_casefold_path TEXT NOT NULL,
  archive_format TEXT NOT NULL CHECK(archive_format IN ('ZIP','SEVEN_Z','ELECTRON_ASAR')),
  compression_profile TEXT NOT NULL CHECK(compression_profile IN (
    'STORE','DEFLATE','SEVEN_Z_DECODER_VALIDATED','ELECTRON_ASAR_STORE','ELECTRON_ASAR_DEFLATE'
  )),
  uncompressed_size_bytes INTEGER NOT NULL CHECK(uncompressed_size_bytes >= 0),
  crc32 TEXT NOT NULL CHECK(length(crc32) = 8),
  md5 TEXT NOT NULL CHECK(length(md5) = 32),
  sha1 TEXT NOT NULL CHECK(length(sha1) = 40),
  sha256 TEXT NOT NULL CHECK(length(sha256) = 64),
  materialized_blob_id TEXT REFERENCES blobs(id),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(archive_blob_id, ordinal),
  UNIQUE(archive_blob_id, normalized_path),
  UNIQUE(archive_blob_id, ascii_casefold_path),
  CHECK((archive_format='ZIP' AND compression_profile IN ('STORE','DEFLATE')) OR
        (archive_format='SEVEN_Z' AND compression_profile='SEVEN_Z_DECODER_VALIDATED') OR
        (archive_format='ELECTRON_ASAR' AND compression_profile IN ('ELECTRON_ASAR_STORE','ELECTRON_ASAR_DEFLATE')))
);

CREATE TABLE "upload_consumptions" (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  consumer_type TEXT NOT NULL CHECK(consumer_type IN (
    'IMPORT_JOB','GAME_FILE_REVISION_JOB','GAME_ASSET','REVIEW_ASSET','REVIEW_ARCADE_PARENT',
    'REVIEW_MULTI_DISC','BIOS_INSTALLATION','RUNTIME_ASSET_PACK_INSTALLATION'
  )),
  consumer_id TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  released_at_ms INTEGER,
  release_reason TEXT CHECK(release_reason IS NULL OR release_reason IN (
    'IMPORT_PUBLISHED','IMPORT_DISCARDED','IMPORT_FAILED_FINAL','IMPORT_CANCELLED',
    'IMPORT_JOB_TERMINAL','PEGASUS_TERMINAL','UPLOAD_CONSUMED','GAME_DELETED'
  )),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  UNIQUE(consumer_type,consumer_id),
  CHECK((released_at_ms IS NULL)=(release_reason IS NULL))
);
