CREATE TABLE upload_sessions (
  id TEXT PRIMARY KEY,
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
);

CREATE TABLE upload_files (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  relative_path TEXT NOT NULL,
  declared_size_bytes INTEGER NOT NULL CHECK(declared_size_bytes BETWEEN 0 AND 8589934592),
  received_size_bytes INTEGER NOT NULL DEFAULT 0 CHECK(received_size_bytes >= 0),
  final_blob_id TEXT REFERENCES blobs(id),
  state TEXT NOT NULL CHECK(state IN ('PENDING','PARTIAL','FINALIZING','COMPLETE','FAILED')),
  last_error_code TEXT,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  UNIQUE(upload_session_id, relative_path),
  CHECK((state = 'COMPLETE') = (final_blob_id IS NOT NULL))
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

CREATE TABLE upload_consumptions (
  id TEXT PRIMARY KEY,
  upload_session_id TEXT NOT NULL REFERENCES upload_sessions(id),
  upload_file_id TEXT REFERENCES upload_files(id),
  consumer_type TEXT NOT NULL CHECK(consumer_type IN ('IMPORT_JOB','GAME_FILE_REVISION_JOB','GAME_ASSET','BIOS_INSTALLATION','DAT_VERSION')),
  consumer_id TEXT NOT NULL,
  created_at_ms INTEGER NOT NULL,
  UNIQUE(consumer_type, consumer_id)
);
CREATE UNIQUE INDEX upload_consumptions_whole_session ON upload_consumptions(upload_session_id) WHERE upload_file_id IS NULL;

CREATE TABLE archive_entries (
  archive_blob_id TEXT NOT NULL REFERENCES blobs(id),
  ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
  original_relative_path TEXT NOT NULL,
  normalized_path TEXT NOT NULL,
  ascii_casefold_path TEXT NOT NULL,
  archive_format TEXT NOT NULL CHECK(archive_format IN ('ZIP','SEVEN_Z')),
  compression_profile TEXT NOT NULL CHECK(compression_profile IN ('STORE','DEFLATE','SEVEN_Z_DECODER_VALIDATED')),
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
        (archive_format='SEVEN_Z' AND compression_profile='SEVEN_Z_DECODER_VALIDATED'))
);

CREATE TRIGGER archive_entries_immutable_update
BEFORE UPDATE ON archive_entries
WHEN OLD.materialized_blob_id IS NOT NULL
  OR NEW.materialized_blob_id IS NULL
  OR NEW.archive_blob_id != OLD.archive_blob_id
  OR NEW.ordinal != OLD.ordinal
  OR NEW.original_relative_path != OLD.original_relative_path
  OR NEW.normalized_path != OLD.normalized_path
  OR NEW.ascii_casefold_path != OLD.ascii_casefold_path
  OR NEW.archive_format != OLD.archive_format
  OR NEW.compression_profile != OLD.compression_profile
  OR NEW.uncompressed_size_bytes != OLD.uncompressed_size_bytes
  OR NEW.crc32 != OLD.crc32 OR NEW.md5 != OLD.md5 OR NEW.sha1 != OLD.sha1 OR NEW.sha256 != OLD.sha256
BEGIN
  SELECT RAISE(ABORT, 'archive entry is immutable');
END;
