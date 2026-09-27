package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/importing"
	"retrom/internal/persistence/filecatalog"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records creationRecords) Artifact(
	ctx context.Context,
	change application.CreationArtifact,
) (string, error) {
	id, err := filecatalog.EnsureRecord(
		ctx,
		records.transaction,
		change.Metadata,
		change.MediaType,
		change.NowMS,
	)
	if err != nil {
		return "", fmt.Errorf("register creation artifact: %w", err)
	}
	return id, nil
}

func (records creationRecords) Archive(
	ctx context.Context,
	archive application.PreparedArchive,
	now int64,
) (map[int]string, error) {
	materialized := make(map[int]string, len(archive.Materialized))
	for ordinal, metadata := range archive.Materialized {
		materialized[ordinal] = metadata.ID
	}
	for _, entry := range archive.Entries {
		if err := records.archiveEntry(ctx, archive.BlobID, entry, now); err != nil {
			return nil, err
		}
	}
	return materialized, nil
}

func (records creationRecords) archiveEntry(
	ctx context.Context,
	id string,
	entry importing.ArchiveEntry,
	now int64,
) error {
	var current importing.ArchiveEntry
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT original_relative_path,normalized_path,ascii_casefold_path,archive_format,compression_profile,
 uncompressed_size_bytes,crc32,md5,sha1,sha256
FROM archive_entries WHERE archive_blob_id=? AND ordinal=?`, id, entry.Ordinal).Scan(
		&current.OriginalPath,
		&current.NormalizedPath,
		&current.ASCIICasefoldPath,
		&current.ArchiveFormat,
		&current.CompressionProfile,
		&current.Size,
		&current.CRC32,
		&current.MD5,
		&current.SHA1,
		&current.SHA256,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return records.insertArchiveEntry(ctx, id, entry, now)
	}
	if err != nil {
		return fmt.Errorf("read existing creation archive entry: %w", err)
	}
	current.Ordinal, current.NestedArchive = entry.Ordinal, entry.NestedArchive
	if current != entry {
		return application.ErrVersionConflict
	}
	return nil
}

func (records creationRecords) insertArchiveEntry(
	ctx context.Context,
	id string,
	entry importing.ArchiveEntry,
	now int64,
) error {
	result, err := recordstore.InsertRows(
		ctx,
		records.transaction,
		"archive_entries",
		`
INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,
 ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
 created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id,
		entry.Ordinal,
		entry.OriginalPath,
		entry.NormalizedPath,
		entry.ASCIICasefoldPath,
		entry.ArchiveFormat,
		entry.CompressionProfile,
		entry.Size,
		entry.CRC32,
		entry.MD5,
		entry.SHA1,
		entry.SHA256,
		now,
	)
	return creationMutation(result, err, "insert creation archive entry", 1)
}

// A copied archive carries its own immutable scan facts; no physical ownership edge remains.
func (records creationRecords) copyArchiveFacts(ctx context.Context, from, to string) error {
	_, err := records.transaction.ExecContext(
		ctx,
		`INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,
ascii_casefold_path,archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,
sha1,sha256,created_at_ms)
SELECT ?,ordinal,original_relative_path,normalized_path,ascii_casefold_path,archive_format,
compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,created_at_ms FROM archive_entries
WHERE archive_blob_id=?
ON CONFLICT(archive_blob_id,ordinal) DO NOTHING`,
		to,
		from,
	)
	if err != nil {
		return fmt.Errorf("copy archive facts to owned file: %w", err)
	}
	return nil
}
