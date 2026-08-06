package datindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/arcadedat"
	"retrom/internal/cleanup"
)

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func Replace(ctx context.Context, transaction *sql.Tx, datID string, catalog arcadedat.Catalog) error {
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM dat_machines
WHERE dat_version_id=?
`, datID); err != nil {
		return fmt.Errorf("datindex/replace: %w", err)
	}
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
		return fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", machineStatement.Close()) }()
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
		return fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", biosStatement.Close()) }()
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
		return fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", romStatement.Close()) }()
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
		return fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", diskStatement.Close()) }()
	for _, machine := range catalog.Machines {
		if _, err := machineStatement.ExecContext(
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
		for _, bios := range machine.BIOSSets {
			if _, err := biosStatement.ExecContext(
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
			if _, err := romStatement.ExecContext(
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
			if _, err := diskStatement.ExecContext(
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
	}
	return nil
}

//nolint:funlen // Requirement upserts and stale-row deactivation must remain one auditable atomic synchronization.
func SyncRequirements(ctx context.Context, transaction *sql.Tx, datID string, now time.Time) error {
	var coreID, artifactID, datSHA256 string
	if err := transaction.QueryRowContext(ctx, `
SELECT core_id,
core_artifact_id,
sha256
FROM dat_versions
WHERE id=?
`, datID).Scan(&coreID, &artifactID, &datSHA256); err != nil {
		return fmt.Errorf(

			// Dependency targets include unresolved romof names. They are preserved as
			// auditable missing slots without inventing a dat_machines row.
			"datindex/replace: %w", err)
	}

	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT machine_name
FROM dat_machines
WHERE dat_version_id=?
AND is_explicit_bios=1
UNION SELECT romof
FROM dat_machines
WHERE dat_version_id=?
AND romof IS NOT NULL
AND romof!=COALESCE(cloneof,
'')
ORDER BY 1
`,
		datID,
		datID,
	)
	if err != nil {
		return fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	machines := make([]string, 0)
	for rows.Next() {
		var machine string
		if err := rows.Scan(&machine); err != nil {
			return fmt.Errorf("datindex/replace: %w", err)
		}
		machines = append(machines, machine)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("datindex/replace: %w", err)
	}
	for _, machine := range machines {
		entries, err := loadRequirementEntries(ctx, transaction, datID, machine)
		if err != nil {
			return err
		}
		canonical, _ := json.Marshal(
			map[string]any{"datSha256": datSHA256, "entries": entries, "machineName": machine, "schemaVersion": 1},
		)
		digest := sha256.Sum256(canonical)
		logicalName := machine + ".zip"
		requirementID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("retrom:bios:"+artifactID+":"+logicalName)).String()
		_, err = transaction.ExecContext(
			ctx,
			`
INSERT INTO bios_requirements(id,
core_id,
core_artifact_id,
source_kind,
dat_machine_name,
logical_name,
requirement_mode,
condition_code,
activation_options_json,
catalog_digest,
size_bytes,
md5,
sha1,
sha256,
source_url,
source_version,
enabled,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
'DAT_MACHINE',
?,
?,
'REQUIRED',
'ARCADE_DAT_DEPENDENCY',
NULL,
?,
NULL,
NULL,
NULL,
NULL,
?,
?,
1,
1,
?,
?) ON CONFLICT(core_artifact_id,
logical_name)
DO UPDATE SET dat_machine_name=excluded.dat_machine_name,
requirement_mode=excluded.requirement_mode,
condition_code=excluded.condition_code,
catalog_digest=excluded.catalog_digest,
source_url=excluded.source_url,
source_version=excluded.source_version,
enabled=1,
version=CASE WHEN bios_requirements.catalog_digest!=excluded.catalog_digest
OR bios_requirements.enabled=0 THEN bios_requirements.version+1 ELSE bios_requirements.version END,
updated_at_ms=excluded.updated_at_ms
`,
			requirementID,
			coreID,
			artifactID,
			machine,
			logicalName,
			hex.EncodeToString(digest[:]),
			fmt.Sprintf("retrom:dat:%s#%s", datID, machine),
			datID,
			now.UnixMilli(),
			now.UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("datindex/replace: %w", err)
		}
	}
	_, err = transaction.ExecContext(
		ctx,
		`
UPDATE bios_requirements
SET enabled=0,
version=version+1,
updated_at_ms=?
WHERE core_artifact_id=?
AND source_kind='DAT_MACHINE'
AND enabled=1
AND source_version!=?
`,
		now.UnixMilli(),
		artifactID,
		datID,
	)
	if err != nil {
		return fmt.Errorf("disable stale DAT BIOS requirements: %w", err)
	}
	return nil
}

func loadRequirementEntries(ctx context.Context, transaction *sql.Tx, datID, machine string) ([]map[string]any, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT r.name,
r.size_bytes,
r.crc32,
r.sha1,
r.status,
r.merge_name,
r.bios_name
FROM dat_rom_entries r
WHERE r.dat_version_id=?
AND r.machine_name=?
AND r.status!='NODUMP'
AND (r.bios_name IS NULL
OR EXISTS(SELECT 1
FROM dat_bios_sets b
WHERE b.dat_version_id=r.dat_version_id
AND b.machine_name=r.machine_name
AND b.bios_name=r.bios_name
AND b.is_default=1))
ORDER BY r.ordinal
`,
		datID,
		machine,
	)
	if err != nil {
		return nil, fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0)
	for rows.Next() {
		var name, status string
		var size int64
		var crc32Value, sha1Value, mergeName, biosName sql.NullString
		if err := rows.Scan(&name, &size, &crc32Value, &sha1Value, &status, &mergeName, &biosName); err != nil {
			return nil, fmt.Errorf("datindex/replace: %w", err)
		}
		entries = append(
			entries,
			map[string]any{
				"biosName":  nullString(biosName),
				"crc32":     nullString(crc32Value),
				"mergeName": nullString(mergeName),
				"name":      name,
				"sha1":      nullString(sha1Value),
				"sizeBytes": size,
				"status":    status,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("datindex/replace: %w", err)
	}
	return entries, nil
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

func nullString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
