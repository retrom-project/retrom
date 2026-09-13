package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/service/metadatascrape"
)

func evidenceSource(subject metadatascrape.Subject) (string, string) {
	if subject.Kind == "GAME" {
		return "game_files", "game_id"
	}
	return "import_item_source_files", "import_item_id"
}

func (reads scheduleReads) Files(
	ctx context.Context,
	subject metadatascrape.Subject,
) ([]metadatascrape.FileEvidence, error) {
	table, column := evidenceSource(subject)
	rows, err := reads.database.QueryContext(ctx, `SELECT s.logical_name,b.id,b.crc32,b.md5,b.sha1,b.sha256,
 s.source_archive_blob_id,s.source_archive_entry_ordinal FROM `+table+` s JOIN blobs b ON b.id=s.blob_id
 WHERE s.`+column+`=? AND s.role='CONTENT' ORDER BY s.sort_order,s.logical_name`, subject.ID)
	if err != nil {
		return nil, fmt.Errorf("query scrape content evidence: %w", err)
	}
	defer func() { cleanup.Error("close scrape content evidence", rows.Close()) }()
	result := make([]metadatascrape.FileEvidence, 0)
	for rows.Next() {
		var item metadatascrape.FileEvidence
		if err := rows.Scan(
			&item.Name,
			&item.BlobID,
			&item.CRC32,
			&item.MD5,
			&item.SHA1,
			&item.SHA256,
			&item.ArchiveBlobID,
			&item.ArchiveOrdinal,
		); err != nil {
			return nil, fmt.Errorf("scan scrape content evidence: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scrape content evidence: %w", err)
	}
	return result, nil
}

func (reads scheduleReads) Arcade(
	ctx context.Context,
	subject metadatascrape.Subject,
	datID, machine string,
) ([]metadatascrape.ArcadeEvidence, error) {
	table, column := evidenceSource(subject)
	rows, err := reads.database.QueryContext(
		ctx,
		`SELECT s.blob_id,e.ordinal,d.name,d.size_bytes,d.crc32,d.sha1
 FROM `+table+` s JOIN archive_entries e ON e.archive_blob_id=s.blob_id
 JOIN dat_rom_entries d ON d.dat_version_id=? AND d.machine_name=? AND d.name=e.normalized_path
 WHERE s.`+column+`=? AND s.role='CONTENT' AND COALESCE(d.status,'GOOD')!='NODUMP'
 AND (d.bios_name IS NULL OR d.bios_name=(SELECT bios_name FROM dat_bios_sets
 WHERE dat_version_id=d.dat_version_id AND machine_name=d.machine_name AND is_default=1))
 AND (d.crc32 IS NOT NULL OR d.sha1 IS NOT NULL) AND e.uncompressed_size_bytes=d.size_bytes
 AND (d.crc32 IS NULL OR lower(e.crc32)=lower(d.crc32)) AND (d.sha1 IS NULL OR lower(e.sha1)=lower(d.sha1))`,
		datID,
		machine,
		subject.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("query arcade scrape evidence: %w", err)
	}
	defer func() { cleanup.Error("close arcade scrape evidence", rows.Close()) }()
	result := make([]metadatascrape.ArcadeEvidence, 0)
	for rows.Next() {
		var item metadatascrape.ArcadeEvidence
		if err := rows.Scan(&item.ArchiveBlobID, &item.Ordinal, &item.Name, &item.Size, &item.CRC32, &item.SHA1); err != nil {
			return nil, fmt.Errorf("scan arcade scrape evidence: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate arcade scrape evidence: %w", err)
	}
	return result, nil
}
