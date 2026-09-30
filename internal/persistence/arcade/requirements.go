package arcade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/content/arcade"
	dbapi "retrom/internal/database"
)

func (records *Catalog) ArcadeRequirements(
	ctx context.Context, datID, machine string,
) (arcade.CatalogRequirements, error) {
	var facts arcade.CatalogRequirements
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT bios_name FROM dat_bios_sets WHERE dat_version_id=? AND machine_name=? AND is_default=1`, datID, machine).
		Scan(&facts.DefaultBIOS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return facts, fmt.Errorf("read DAT default BIOS: %w", err)
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT name,COALESCE(status,'GOOD'),bios_name,size_bytes,crc32,sha1,merge_name FROM dat_rom_entries
WHERE dat_version_id=? AND machine_name=? ORDER BY ordinal`, datID, machine)
	if err != nil {
		return facts, fmt.Errorf("query DAT arcade ROMs: %w", err)
	}
	defer func() { cleanup.Error("close DAT arcade ROMs", rows.Close()) }()
	for rows.Next() {
		var rom arcade.ROMRequirement
		if err := rows.Scan(
			&rom.Name, &rom.Status, &rom.BIOSName, &rom.Size, &rom.CRC32, &rom.SHA1, &rom.MergeName,
		); err != nil {
			return arcade.CatalogRequirements{}, fmt.Errorf("scan DAT arcade ROM: %w", err)
		}
		facts.ROMs = append(facts.ROMs, rom)
	}
	if err := rows.Err(); err != nil {
		return arcade.CatalogRequirements{}, fmt.Errorf("iterate DAT arcade ROMs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return arcade.CatalogRequirements{}, fmt.Errorf("close DAT arcade ROMs: %w", err)
	}
	err = dbapi.QueryRowContext(ctx, records.executor, `
SELECT EXISTS(SELECT 1 FROM dat_disk_entries
 WHERE dat_version_id=? AND machine_name=? AND COALESCE(status,'GOOD')!='NODUMP')`,
		datID, machine).Scan(&facts.HasDisk)
	if err != nil {
		return arcade.CatalogRequirements{}, fmt.Errorf("read DAT arcade disks: %w", err)
	}
	return facts, nil
}
