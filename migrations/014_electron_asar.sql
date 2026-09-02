-- Add explicit evidence for project files streamed from Electron app.asar bundles.

PRAGMA defer_foreign_keys = ON;

CREATE TABLE archive_entries_next (
  archive_blob_id TEXT NOT NULL REFERENCES blobs(id),
  ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
  original_relative_path TEXT NOT NULL,
  normalized_path TEXT NOT NULL,
  ascii_casefold_path TEXT NOT NULL,
  archive_format TEXT NOT NULL CHECK(archive_format IN ('ZIP','SEVEN_Z','ELECTRON_ASAR')),
  compression_profile TEXT NOT NULL CHECK(compression_profile IN (
    'STORE','DEFLATE','SEVEN_Z_DECODER_VALIDATED',
    'ELECTRON_ASAR_STORE','ELECTRON_ASAR_DEFLATE'
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
        (archive_format='ELECTRON_ASAR' AND compression_profile IN (
          'ELECTRON_ASAR_STORE','ELECTRON_ASAR_DEFLATE'
        )))
);

INSERT INTO archive_entries_next(
  archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
  archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
  materialized_blob_id,created_at_ms
)
SELECT archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
       archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
       materialized_blob_id,created_at_ms
FROM archive_entries;

DROP TABLE archive_entries;
ALTER TABLE archive_entries_next RENAME TO archive_entries;

CREATE INDEX fk_archive_entries_materialized ON archive_entries(materialized_blob_id);

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
