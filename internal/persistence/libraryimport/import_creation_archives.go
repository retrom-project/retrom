package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"retrom/internal/importing"
	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records creationRecords) Artifact(
	ctx context.Context,
	change application.CreationArtifact,
) (string, error) {
	id, err := blobcatalog.EnsureRecord(ctx, records.transaction, change.Metadata, change.MediaType, change.NowMS)
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
	ordinals := make([]int, 0, len(archive.Materialized))
	for ordinal := range archive.Materialized {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	for _, ordinal := range ordinals {
		id, err := records.Artifact(
			ctx,
			application.CreationArtifact{
				Metadata:  archive.Materialized[ordinal],
				MediaType: "application/octet-stream",
				NowMS:     now,
			},
		)
		if err != nil {
			return nil, err
		}
		materialized[ordinal] = id
	}
	for _, entry := range archive.Entries {
		if err := records.archiveEntry(ctx, archive.BlobID, entry, materialized[entry.Ordinal], now); err != nil {
			return nil, err
		}
	}
	return materialized, nil
}

func (records creationRecords) archiveEntry(
	ctx context.Context,
	id string,
	entry importing.ArchiveEntry,
	blobID string,
	now int64,
) error {
	var current importing.ArchiveEntry
	var currentBlob *string
	err := records.transaction.QueryRowContext(ctx, `
SELECT original_relative_path,normalized_path,ascii_casefold_path,archive_format,compression_profile,
 uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id
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
		&currentBlob,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return records.insertArchiveEntry(ctx, id, entry, blobID, now)
	}
	if err != nil {
		return fmt.Errorf("read existing creation archive entry: %w", err)
	}
	current.Ordinal, current.NestedArchive = entry.Ordinal, entry.NestedArchive
	if current != entry {
		return application.ErrVersionConflict
	}
	if blobID == "" {
		return nil
	}
	if currentBlob != nil {
		if *currentBlob != blobID {
			return application.ErrVersionConflict
		}
		return nil
	}
	result, err := recordstore.UpdateArchiveEntries(
		ctx,
		records.transaction,
		recordstore.Update{
			Set: `materialized_blob_id=?`,
			Scope: recordstore.Scope{
				Where: `archive_blob_id=? AND ordinal=? AND materialized_blob_id IS NULL`,
				Args:  []any{id, entry.Ordinal},
			},
			Values: []any{blobID},
		},
	)
	return creationMutation(result, err, "attach creation archive artifact", 1)
}

func (records creationRecords) insertArchiveEntry(
	ctx context.Context,
	id string,
	entry importing.ArchiveEntry,
	blobID string,
	now int64,
) error {
	result, err := records.transaction.ExecContext(
		ctx,
		`
INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,
 ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
 materialized_blob_id,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
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
		creationNullable(blobID),
		now,
	)
	return creationMutation(result, err, "insert creation archive entry", 1)
}
