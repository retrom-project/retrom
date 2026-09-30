package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/content/arcade"
	arcaderecords "retrom/internal/persistence/arcade"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records approvalDependencyRecords) LogicalName(ctx context.Context, snapshotID string) (string, error) {
	var name string
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT logical_name FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','DOS_SOURCE','PROJECT_FILE')
ORDER BY CASE role WHEN 'CONTENT' THEN 0 WHEN 'DISC' THEN 1 ELSE 2 END,sort_order,logical_name LIMIT 1`,
		snapshotID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", libraryservice.ErrInvalid
	}
	if err != nil {
		return "", fmt.Errorf("read approval content name: %w", err)
	}
	if name == "" {
		return "", libraryservice.ErrInvalid
	}
	return name, nil
}

func (records approvalDependencyRecords) MultiDisc(
	ctx context.Context, snapshotID, itemID string,
) (libraryservice.ApprovalMultiDisc, error) {
	var facts libraryservice.ApprovalMultiDisc
	rows, err := records.executor.QueryContext(ctx, `
SELECT entry.ordinal,entry.state,entry.file_record,entry.source_logical_name,
 file.file_record,file.logical_name,file.sort_order,json_extract(blob.value, '$.size_bytes')
FROM import_item_multidisc_entries entry
LEFT JOIN import_item_source_snapshot_files file ON file.source_snapshot_id=entry.source_snapshot_id
 AND file.role='DISC' AND file.sort_order=entry.ordinal
LEFT JOIN json_each(json_array(entry.file_record)) blob ON blob.value IS NOT NULL
WHERE entry.source_snapshot_id=? ORDER BY entry.ordinal`, snapshotID)
	if err != nil {
		return facts, fmt.Errorf("query approval discs: %w", err)
	}
	defer func() { cleanup.Error("close approval discs", rows.Close()) }()
	for rows.Next() {
		var disc libraryservice.ApprovalDisc
		if err := rows.Scan(&disc.Ordinal, &disc.State, &disc.FileRecord, &disc.LogicalName,
			&disc.SourceFileRecord, &disc.SourceLogicalName, &disc.SourceOrdinal, &disc.SizeBytes); err != nil {
			return libraryservice.ApprovalMultiDisc{}, fmt.Errorf("scan approval disc: %w", err)
		}
		facts.Discs = append(facts.Discs, disc)
	}
	if err := rows.Err(); err != nil {
		return libraryservice.ApprovalMultiDisc{}, fmt.Errorf("iterate approval discs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return libraryservice.ApprovalMultiDisc{}, fmt.Errorf("close approval discs: %w", err)
	}
	err = dbapi.QueryRowContext(ctx, records.executor, `
SELECT count(*) FILTER(WHERE role='PLAYLIST_SOURCE'),count(*) FILTER(WHERE role='DISC'),count(*),
 (SELECT count(*) FROM import_item_runtime_files
  WHERE import_item_id=? AND role='MULTI_DISC_PLAYLIST')
FROM import_item_source_snapshot_files WHERE source_snapshot_id=?`, itemID, snapshotID).
		Scan(&facts.PlaylistCount, &facts.DiscCount, &facts.SourceCount, &facts.CanonicalCount)
	if err != nil {
		return libraryservice.ApprovalMultiDisc{}, fmt.Errorf("read approval disc counts: %w", err)
	}
	return facts, nil
}

func (records approvalDependencyRecords) ArcadeRequirements(
	ctx context.Context, datID, machine string,
) (arcade.CatalogRequirements, error) {
	facts, err := arcaderecords.New(records.executor).ArcadeRequirements(ctx, datID, machine)
	if err != nil {
		return arcade.CatalogRequirements{}, fmt.Errorf("read approval arcade requirements: %w", err)
	}
	return facts, nil
}

func (records approvalDependencyRecords) ExternalFileCount(
	ctx context.Context, itemID, role, name string,
) (int64, error) {
	files, err := ReadReviewRuntimeFiles(ctx, records.executor, itemID)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, file := range files {
		if file.Role == role && file.LogicalName == name {
			count++
		}
	}
	return count, nil
}
