package libraryimport

import (
	"fmt"

	"retrom/internal/blobstore"
)

const insertArchiveEntrySQL = `
INSERT OR IGNORE INTO archive_entries(
  archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
  archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
  materialized_blob_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`

func (run *creationRun) persistArchives() error {
	for _, archive := range run.plan.archives {
		materialized, err := run.materializeArchive(archive)
		if err != nil {
			return err
		}
		if err := run.persistArchiveEntries(archive, materialized); err != nil {
			return err
		}
	}
	return nil
}

func (run *creationRun) materializeArchive(archive preparedArchive) (map[int]string, error) {
	result := make(map[int]string, len(archive.materialized))
	for ordinal, metadata := range archive.materialized {
		blobID, err := blobstore.EnsureRecord(
			run.ctx, run.transaction, metadata, "application/octet-stream", run.now,
		)
		if err != nil {
			return nil, fmt.Errorf("libraryimport/service: %w", err)
		}
		result[ordinal] = blobID
		run.materialized[fmt.Sprintf("%s:%d", archive.blobID, ordinal)] = blobID
	}
	return result, nil
}

func (run *creationRun) persistArchiveEntries(archive preparedArchive, materialized map[int]string) error {
	for _, entry := range archive.entries {
		blobID := materialized[entry.Ordinal]
		_, err := run.transaction.ExecContext(
			run.ctx,
			insertArchiveEntrySQL,
			archive.blobID, entry.Ordinal, entry.OriginalPath, entry.NormalizedPath,
			entry.ASCIICasefoldPath, entry.ArchiveFormat, entry.CompressionProfile, entry.Size,
			entry.CRC32, entry.MD5, entry.SHA1, entry.SHA256, nullableText(blobID), run.now,
		)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		if err := run.attachMaterializedArchiveEntry(archive.blobID, entry.Ordinal, blobID); err != nil {
			return err
		}
	}
	return nil
}

func (run *creationRun) attachMaterializedArchiveEntry(archiveBlobID string, ordinal int, blobID string) error {
	if blobID == "" {
		return nil
	}
	_, err := run.transaction.ExecContext(run.ctx, `
UPDATE archive_entries
SET materialized_blob_id=?
WHERE archive_blob_id=? AND ordinal=? AND materialized_blob_id IS NULL
`, blobID, archiveBlobID, ordinal)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}
