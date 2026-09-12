package recordstore

import (
	"context"
	"database/sql"
)

func UpdateArchiveEntries(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"archive_entries",
		"archive_blob_id,ordinal,archive_format,ascii_casefold_path,compression_profile,crc32,"+
			"materialized_blob_id,md5,normalized_path,original_relative_path,sha1,sha256,"+
			"uncompressed_size_bytes",
		ArchiveEntriesUpdateRule,
	)
}

const ArchiveEntriesUpdateRule = `
WITH previous(archive_blob_id,ordinal,archive_format,ascii_casefold_path,compression_profile,crc32,
materialized_blob_id,md5,normalized_path,original_relative_path,sha1,sha256,uncompressed_size_bytes) AS
(VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- archive_entries_immutable_update
WHEN (previous.materialized_blob_id IS NOT NULL
  OR candidate.materialized_blob_id IS NULL
  OR candidate.archive_blob_id != previous.archive_blob_id
  OR candidate.ordinal != previous.ordinal
  OR candidate.original_relative_path != previous.original_relative_path
  OR candidate.normalized_path != previous.normalized_path
  OR candidate.ascii_casefold_path != previous.ascii_casefold_path
  OR candidate.archive_format != previous.archive_format
  OR candidate.compression_profile != previous.compression_profile
  OR candidate.uncompressed_size_bytes != previous.uncompressed_size_bytes
  OR candidate.crc32 != previous.crc32 OR candidate.md5 != previous.md5 OR candidate.sha1 !=
previous.sha1 OR candidate.sha256 != previous.sha256) THEN 'archive entry is immutable'
ELSE '' END
FROM archive_entries candidate CROSS JOIN previous
WHERE candidate.archive_blob_id=previous.archive_blob_id AND candidate.ordinal=previous.ordinal`
