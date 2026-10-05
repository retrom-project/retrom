package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewSources struct{ executor dbapi.Executor }

func (records ReviewSources) Files(
	ctx context.Context, snapshotID string,
) ([]libraryservice.ReviewSourceRecord, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT f.id,f.relative_path,(((b.value)::jsonb #>> '{size_bytes}'))::bigint,((b.value)::jsonb #>> '{sha256}'),
((b.value)::jsonb #>> '{md5}'),((b.value)::jsonb #>> '{crc32}'),
max(CASE WHEN s.source_archive_file_record IS NOT NULL OR EXISTS(
  SELECT 1 FROM archive_entries ae WHERE ae.archive_file_record=b.value
) THEN 1 ELSE 0 END),
COALESCE(
  max(s.source_archive_file_record),
  max(CASE WHEN EXISTS(
    SELECT 1 FROM archive_entries ae WHERE ae.archive_file_record=b.value
  ) THEN b.value END)
)
FROM import_item_source_snapshot_files s
JOIN import_files f ON f.id=s.upload_file_id
JOIN LATERAL (SELECT COALESCE(s.source_archive_file_record,s.file_record) AS value) b ON b.value IS NOT NULL
WHERE s.source_snapshot_id=?
GROUP BY f.id,f.relative_path,(((b.value)::jsonb #>> '{size_bytes}'))::bigint,((b.value)::jsonb #>> '{sha256}'),
((b.value)::jsonb #>> '{md5}'),((b.value)::jsonb #>> '{crc32}')
ORDER BY min(s.sort_order),f.relative_path,f.id
`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query review source files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]libraryservice.ReviewSourceRecord, 0)
	for rows.Next() {
		var row libraryservice.ReviewSourceRecord
		if err := rows.Scan(&row.ID,
			&row.Name,
			&row.SizeBytes,
			&row.SHA256,
			&row.MD5,
			&row.CRC32,
			&row.Archive,
			&row.ArchiveFileRecord); err != nil {
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
	archiveFileRecord string,
) (libraryservice.ReviewArchive, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT original_relative_path,uncompressed_size_bytes,crc32,archive_format
FROM archive_entries
WHERE archive_file_record=?
ORDER BY ordinal
`, archiveFileRecord)
	if err != nil {
		return libraryservice.ReviewArchive{}, fmt.Errorf("query review archive entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := libraryservice.ReviewArchive{Entries: []libraryservice.ReviewArchiveEntry{}}
	for rows.Next() {
		var row libraryservice.ReviewArchiveEntry
		var format string
		if err := rows.Scan(&row.Name, &row.SizeBytes, &row.CRC32, &format); err != nil {
			return libraryservice.ReviewArchive{}, fmt.Errorf("scan review archive entry: %w", err)
		}
		result.Format = &format
		result.Entries = append(result.Entries, row)
	}
	if err := rows.Err(); err != nil {
		return libraryservice.ReviewArchive{}, fmt.Errorf("iterate review archive entries: %w", err)
	}
	return result, nil
}
