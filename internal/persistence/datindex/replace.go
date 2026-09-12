package datindex

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/arcadedat"
	"retrom/internal/cleanup"
)

func Replace(ctx context.Context, transaction *sql.Tx, datID string, catalog arcadedat.Catalog) error {
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM dat_machines
WHERE dat_version_id=?
`, datID); err != nil {
		return fmt.Errorf("datindex/replace: %w", err)
	}
	statements, err := prepareReplacementStatements(ctx, transaction)
	if err != nil {
		return err
	}
	defer statements.close()
	for _, machine := range catalog.Machines {
		if _, err := statements.machine.ExecContext(
			ctx,
			datID,
			machine.Name,
			machine.Description,
			machine.Year,
			machine.Manufacturer,
			nullable(machine.CloneOf),
			nullable(machine.ROMOf),
			boolInteger(machine.ExplicitBIOS),
			machine.Classification,
		); err != nil {
			return fmt.Errorf("datindex/replace: %w", err)
		}
		if err := insertMachineContents(ctx, statements, datID, machine); err != nil {
			return err
		}
	}
	return nil
}

type replacementStatements struct {
	machine *sql.Stmt
	bios    *sql.Stmt
	rom     *sql.Stmt
	disk    *sql.Stmt
}

func prepareReplacementStatements(ctx context.Context, transaction *sql.Tx) (replacementStatements, error) {
	var statements replacementStatements
	machineStatement, err := transaction.PrepareContext(
		ctx,
		`
INSERT INTO dat_machines(dat_version_id,
machine_name,
description,
year,
manufacturer,
cloneof,
romof,
is_explicit_bios,
classification) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
	)
	if err != nil {
		return statements, fmt.Errorf("datindex/replace: %w", err)
	}
	statements.machine = machineStatement
	biosStatement, err := transaction.PrepareContext(
		ctx,
		`
INSERT INTO dat_bios_sets(dat_version_id,
machine_name,
bios_name,
description,
is_default) VALUES(?,
?,
?,
?,
?)
`,
	)
	if err != nil {
		statements.close()
		return statements, fmt.Errorf("datindex/replace: %w", err)
	}
	statements.bios = biosStatement
	romStatement, err := transaction.PrepareContext(
		ctx,
		`
INSERT INTO dat_rom_entries(dat_version_id,
machine_name,
ordinal,
name,
size_bytes,
crc32,
sha1,
status,
merge_name,
bios_name) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
	)
	if err != nil {
		statements.close()
		return statements, fmt.Errorf("datindex/replace: %w", err)
	}
	statements.rom = romStatement
	diskStatement, err := transaction.PrepareContext(
		ctx,
		`
INSERT INTO dat_disk_entries(dat_version_id,
machine_name,
ordinal,
name,
sha1,
status) VALUES(?,
?,
?,
?,
?,
?)
`,
	)
	if err != nil {
		statements.close()
		return statements, fmt.Errorf("datindex/replace: %w", err)
	}
	statements.disk = diskStatement
	return statements, nil
}

func (statements replacementStatements) close() {
	for _, statement := range []*sql.Stmt{statements.machine, statements.bios, statements.rom, statements.disk} {
		if statement != nil {
			cleanup.Error("close", statement.Close())
		}
	}
}

func insertMachineContents(
	ctx context.Context,
	statements replacementStatements,
	datID string,
	machine arcadedat.Machine,
) error {
	for _, bios := range machine.BIOSSets {
		if _, err := statements.bios.ExecContext(
			ctx,
			datID,
			machine.Name,
			bios.Name,
			bios.Description,
			boolInteger(bios.Default),
		); err != nil {
			return fmt.Errorf("datindex/replace: %w", err)
		}
	}
	for _, rom := range machine.ROMs {
		if _, err := statements.rom.ExecContext(
			ctx,
			datID,
			machine.Name,
			rom.Ordinal,
			rom.Name,
			rom.SizeBytes,
			nullable(rom.CRC32),
			nullable(rom.SHA1),
			rom.Status,
			nullable(rom.MergeName),
			nullable(rom.BIOSName),
		); err != nil {
			return fmt.Errorf("datindex/replace: %w", err)
		}
	}
	for _, disk := range machine.Disks {
		if _, err := statements.disk.ExecContext(
			ctx,
			datID,
			machine.Name,
			disk.Ordinal,
			disk.Name,
			nullable(disk.SHA1),
			disk.Status,
		); err != nil {
			return fmt.Errorf("datindex/replace: %w", err)
		}
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
