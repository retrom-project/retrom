package firmware

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/firmware"
	"retrom/internal/importing"
)

func (store requirementRecords) DATEntries(ctx context.Context, id string) ([]firmware.ExpectedDATEntry, error) {
	rows, err := store.executor.QueryContext(ctx, `
SELECT r.name,r.size_bytes,COALESCE(r.crc32,''),COALESCE(r.sha1,'')
FROM dat_rom_entries r JOIN dat_versions d ON d.id=r.dat_version_id
JOIN bios_requirements q ON q.provider_id=d.provider_id AND q.target_id=d.target_id
 AND q.dat_machine_name=r.machine_name AND q.source_version=r.dat_version_id
WHERE q.id=? AND COALESCE(r.status,'GOOD')!='NODUMP' AND (r.bios_name IS NULL OR EXISTS(
 SELECT 1 FROM dat_bios_sets b WHERE b.dat_version_id=r.dat_version_id AND b.machine_name=r.machine_name
 AND b.bios_name=r.bios_name AND b.is_default=1)) ORDER BY r.name COLLATE BINARY,r.ordinal`, id)
	if err != nil {
		return nil, fmt.Errorf("query BIOS DAT entries: %w", err)
	}
	defer func() { cleanup.Error("close BIOS DAT entries", rows.Close()) }()
	result := make([]firmware.ExpectedDATEntry, 0)
	for rows.Next() {
		var value firmware.ExpectedDATEntry
		if err := rows.Scan(&value.Name, &value.SizeBytes, &value.CRC32, &value.SHA1); err != nil {
			return nil, fmt.Errorf("scan BIOS DAT entry: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate BIOS DAT entries: %w", err)
	}
	return result, nil
}

func (store archiveRecords) Entries(ctx context.Context, id string) ([]importing.ArchiveEntry, error) {
	rows, err := store.executor.QueryContext(
		ctx,
		`SELECT ordinal,original_relative_path,normalized_path,ascii_casefold_path,
 archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256
FROM archive_entries WHERE archive_blob_id=? ORDER BY ordinal`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("query BIOS archive facts: %w", err)
	}
	defer func() { cleanup.Error("close BIOS archive facts", rows.Close()) }()
	result := make([]importing.ArchiveEntry, 0)
	for rows.Next() {
		var entry importing.ArchiveEntry
		if err := rows.Scan(
			&entry.Ordinal,
			&entry.OriginalPath,
			&entry.NormalizedPath,
			&entry.ASCIICasefoldPath,

			&entry.ArchiveFormat,
			&entry.CompressionProfile,
			&entry.Size,
			&entry.CRC32,
			&entry.MD5,
			&entry.SHA1,
			&entry.SHA256,
		); err != nil {
			return nil, fmt.Errorf("scan BIOS archive facts: %w", err)
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate BIOS archive facts: %w", err)
	}
	return result, nil
}

func (store writes) Put(ctx context.Context, id string, entries []importing.ArchiveEntry, now int64) error {
	for _, entry := range entries {
		if _, err := store.transaction.ExecContext(
			ctx,
			`INSERT INTO archive_entries(archive_blob_id,ordinal,
original_relative_path,normalized_path,ascii_casefold_path,archive_format,compression_profile,
uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,?) ON CONFLICT(archive_blob_id,ordinal) DO NOTHING`,

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
		); err != nil {
			return fmt.Errorf("persist BIOS archive facts: %w", err)
		}
	}
	return nil
}
