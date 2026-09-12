package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/libraryimport"
)

func (records approvalDependencyRecords) LogicalName(ctx context.Context, snapshotID string) (string, error) {
	var name string
	err := records.executor.QueryRowContext(ctx, `
SELECT logical_name FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','DOS_SOURCE')
ORDER BY CASE role WHEN 'CONTENT' THEN 0 WHEN 'DISC' THEN 1 ELSE 2 END,sort_order,logical_name LIMIT 1`,
		snapshotID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", application.ErrInvalid
	}
	if err != nil {
		return "", fmt.Errorf("read approval content name: %w", err)
	}
	if name == "" {
		return "", application.ErrInvalid
	}
	return name, nil
}

func (records approvalDependencyRecords) MultiDisc(
	ctx context.Context, snapshotID, validationID string,
) (application.ApprovalMultiDisc, error) {
	var facts application.ApprovalMultiDisc
	rows, err := records.executor.QueryContext(ctx, `
SELECT entry.ordinal,entry.state,entry.blob_id,entry.source_logical_name,
 file.blob_id,file.logical_name,file.sort_order,blob.size_bytes
FROM import_item_multidisc_entries entry
LEFT JOIN import_item_source_snapshot_files file ON file.source_snapshot_id=entry.source_snapshot_id
 AND file.role='DISC' AND file.sort_order=entry.ordinal
LEFT JOIN blobs blob ON blob.id=entry.blob_id
WHERE entry.source_snapshot_id=? ORDER BY entry.ordinal`, snapshotID)
	if err != nil {
		return facts, fmt.Errorf("query approval discs: %w", err)
	}
	defer func() { cleanup.Error("close approval discs", rows.Close()) }()
	for rows.Next() {
		var disc application.ApprovalDisc
		if err := rows.Scan(&disc.Ordinal, &disc.State, &disc.BlobID, &disc.LogicalName,
			&disc.SourceBlobID, &disc.SourceLogicalName, &disc.SourceOrdinal, &disc.SizeBytes); err != nil {
			return application.ApprovalMultiDisc{}, fmt.Errorf("scan approval disc: %w", err)
		}
		facts.Discs = append(facts.Discs, disc)
	}
	if err := rows.Err(); err != nil {
		return application.ApprovalMultiDisc{}, fmt.Errorf("iterate approval discs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return application.ApprovalMultiDisc{}, fmt.Errorf("close approval discs: %w", err)
	}
	err = records.executor.QueryRowContext(ctx, `
SELECT count(*) FILTER(WHERE role='PLAYLIST_SOURCE'),count(*) FILTER(WHERE role='DISC'),count(*),
 (SELECT count(*) FROM import_item_validation_files
  WHERE import_item_core_validation_id=? AND role='MULTI_DISC_PLAYLIST')
FROM import_item_source_snapshot_files WHERE source_snapshot_id=?`, validationID, snapshotID).
		Scan(&facts.PlaylistCount, &facts.DiscCount, &facts.SourceCount, &facts.CanonicalCount)
	if err != nil {
		return application.ApprovalMultiDisc{}, fmt.Errorf("read approval disc counts: %w", err)
	}
	return facts, nil
}

func (records approvalDependencyRecords) ArcadeRequirements(
	ctx context.Context, datID, machine string,
) (application.ApprovalArcadeRequirements, error) {
	var facts application.ApprovalArcadeRequirements
	err := records.executor.QueryRowContext(ctx, `
SELECT bios_name FROM dat_bios_sets WHERE dat_version_id=? AND machine_name=? AND is_default=1`, datID, machine).
		Scan(&facts.DefaultBIOS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return facts, fmt.Errorf("read approval default BIOS: %w", err)
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT name,COALESCE(status,'GOOD'),bios_name FROM dat_rom_entries
WHERE dat_version_id=? AND machine_name=? ORDER BY ordinal`, datID, machine)
	if err != nil {
		return facts, fmt.Errorf("query approval arcade ROMs: %w", err)
	}
	defer func() { cleanup.Error("close approval arcade ROMs", rows.Close()) }()
	for rows.Next() {
		var rom application.ApprovalArcadeROM
		if err := rows.Scan(&rom.Name, &rom.Status, &rom.BIOSName); err != nil {
			return application.ApprovalArcadeRequirements{}, fmt.Errorf("scan approval arcade ROM: %w", err)
		}
		facts.ROMs = append(facts.ROMs, rom)
	}
	if err := rows.Err(); err != nil {
		return application.ApprovalArcadeRequirements{}, fmt.Errorf("iterate approval arcade ROMs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return application.ApprovalArcadeRequirements{}, fmt.Errorf("close approval arcade ROMs: %w", err)
	}
	err = records.executor.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM dat_disk_entries
 WHERE dat_version_id=? AND machine_name=? AND COALESCE(status,'GOOD')!='NODUMP')`,
		datID, machine).Scan(&facts.HasDisk)
	if err != nil {
		return application.ApprovalArcadeRequirements{}, fmt.Errorf("read approval arcade disks: %w", err)
	}
	return facts, nil
}

func (records approvalDependencyRecords) ExternalFileCount(
	ctx context.Context, validationID, role, name string,
) (int64, error) {
	var count int64
	err := records.executor.QueryRowContext(ctx, `
SELECT count(*) FROM import_item_validation_files WHERE import_item_core_validation_id=? AND role=? AND logical_name=?`,
		validationID, role, name).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("read approval external file count: %w", err)
	}
	return count, nil
}
