package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

type ReviewSources struct{ executor dbexec.Executor }

func (records ReviewSources) Files(ctx context.Context, snapshotID string) ([]application.ReviewSourceRecord, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32,
MAX(CASE WHEN s.source_archive_blob_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM archive_entries ae WHERE ae.archive_blob_id=f.final_blob_id
) THEN 1 ELSE 0 END),
COALESCE(
  MAX(s.source_archive_blob_id),
  MAX(CASE WHEN EXISTS(
    SELECT 1 FROM archive_entries ae WHERE ae.archive_blob_id=f.final_blob_id
  ) THEN f.final_blob_id END)
)
FROM import_item_source_snapshot_files s
JOIN upload_files f ON f.id=s.upload_file_id
JOIN blobs b ON b.id=f.final_blob_id
WHERE s.source_snapshot_id=?
GROUP BY f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32
ORDER BY min(s.sort_order),f.relative_path,f.id
`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query review source files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]application.ReviewSourceRecord, 0)
	for rows.Next() {
		var row application.ReviewSourceRecord
		if err := rows.Scan(&row.ID,
			&row.Name,
			&row.SizeBytes,
			&row.SHA256,
			&row.MD5,
			&row.CRC32,
			&row.Archive,
			&row.ArchiveBlobID); err != nil {
			return nil, fmt.Errorf("scan review source file: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review source files: %w", err)
	}
	return result, nil
}

func (records ReviewSources) ArchiveEntries(
	ctx context.Context,
	archiveBlobID string,
) (application.ReviewArchive, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT original_relative_path,uncompressed_size_bytes,crc32,archive_format
FROM archive_entries
WHERE archive_blob_id=?
ORDER BY ordinal
`, archiveBlobID)
	if err != nil {
		return application.ReviewArchive{}, fmt.Errorf("query review archive entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := application.ReviewArchive{Entries: []application.ReviewArchiveEntry{}}
	for rows.Next() {
		var row application.ReviewArchiveEntry
		var format string
		if err := rows.Scan(&row.Name, &row.SizeBytes, &row.CRC32, &format); err != nil {
			return application.ReviewArchive{}, fmt.Errorf("scan review archive entry: %w", err)
		}
		result.Format = &format
		result.Entries = append(result.Entries, row)
	}
	if err := rows.Err(); err != nil {
		return application.ReviewArchive{}, fmt.Errorf("iterate review archive entries: %w", err)
	}
	return result, nil
}
