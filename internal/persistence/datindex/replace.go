package datindex

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/format/arcadedat"
)

func Replace(ctx context.Context, transaction dbapi.Tx, datID string, catalog arcadedat.Catalog) error {
	if _, err := transaction.ExecContext(ctx, "DELETE FROM dat_machines WHERE dat_version_id=?", datID); err != nil {
		return fmt.Errorf("datindex/replace: %w", err)
	}
	// Publish every parent before child batches so PostgreSQL checks all foreign
	// keys normally. The caller owns one transaction for the entire replacement.
	for _, insert := range []func(context.Context, dbapi.Tx, string, arcadedat.Catalog) error{
		insertMachines, insertBIOSSets, insertROMEntries, insertDiskEntries,
	} {
		if err := insert(ctx, transaction, datID, catalog); err != nil {
			return err
		}
	}
	return nil
}

func insertMachines(ctx context.Context, tx dbapi.Tx, datID string, catalog arcadedat.Catalog) error {
	batch := catalogBatch{executor: tx, insert: `INSERT INTO dat_machines(
dat_version_id,machine_name,description,year,manufacturer,cloneof,romof,is_explicit_bios,classification)`}
	for _, machine := range catalog.Machines {
		if err := batch.add(ctx, datID, machine.Name, machine.Description, machine.Year, machine.Manufacturer,
			nullable(machine.CloneOf), nullable(machine.ROMOf), boolInteger(machine.ExplicitBIOS), machine.Classification); err != nil {
			return err
		}
	}
	return batch.flush(ctx)
}

func insertBIOSSets(ctx context.Context, tx dbapi.Tx, datID string, catalog arcadedat.Catalog) error {
	batch := catalogBatch{executor: tx, insert: `INSERT INTO dat_bios_sets(
dat_version_id,machine_name,bios_name,description,is_default)`}
	for _, machine := range catalog.Machines {
		for _, bios := range machine.BIOSSets {
			if err := batch.add(ctx, datID, machine.Name, bios.Name, bios.Description, boolInteger(bios.Default)); err != nil {
				return err
			}
		}
	}
	return batch.flush(ctx)
}

func insertROMEntries(ctx context.Context, tx dbapi.Tx, datID string, catalog arcadedat.Catalog) error {
	batch := catalogBatch{executor: tx, insert: `INSERT INTO dat_rom_entries(
dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status,merge_name,bios_name)`}
	for _, machine := range catalog.Machines {
		for _, rom := range machine.ROMs {
			if err := batch.add(ctx, datID, machine.Name, rom.Ordinal, rom.Name, rom.SizeBytes,
				nullable(rom.CRC32), nullable(rom.SHA1), rom.Status, nullable(rom.MergeName), nullable(rom.BIOSName)); err != nil {
				return err
			}
		}
	}
	return batch.flush(ctx)
}

func insertDiskEntries(ctx context.Context, tx dbapi.Tx, datID string, catalog arcadedat.Catalog) error {
	batch := catalogBatch{executor: tx, insert: `INSERT INTO dat_disk_entries(
dat_version_id,machine_name,ordinal,name,sha1,status)`}
	for _, machine := range catalog.Machines {
		for _, disk := range machine.Disks {
			if err := batch.add(ctx, datID, machine.Name, disk.Ordinal, disk.Name, nullable(disk.SHA1), disk.Status); err != nil {
				return err
			}
		}
	}
	return batch.flush(ctx)
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
