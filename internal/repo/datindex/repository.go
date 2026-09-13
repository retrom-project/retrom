package datindex

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	service "retrom/internal/service/datindex"
)

type Records struct{ executor dbexec.Executor }

// Bind joins requirement publication to the caller's transaction without committing it.
func Bind(transaction *sql.Tx) Records { return Records{executor: transaction} }

func (store Records) Definition(ctx context.Context, datID string) (service.Definition, error) {
	var definition service.Definition
	if err := store.executor.QueryRowContext(ctx, `
SELECT core_id,
provider_id,
target_id,
sha256
FROM dat_versions
WHERE id=?
`, datID).Scan(&definition.CoreID, &definition.ProviderID, &definition.TargetID, &definition.SHA256); err != nil {
		return service.Definition{}, fmt.Errorf("datindex/read definition: %w", err)
	}
	return definition, nil
}

func (store Records) MachineNames(ctx context.Context, datID string) ([]string, error) {
	rows, err := store.executor.QueryContext(
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
		return nil, fmt.Errorf("datindex/replace: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	machines := make([]string, 0)
	for rows.Next() {
		var machine string
		if err := rows.Scan(&machine); err != nil {
			return nil, fmt.Errorf("datindex/replace: %w", err)
		}
		machines = append(machines, machine)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("datindex/replace: %w", err)
	}
	return machines, nil
}

func (store Records) RequiredEntries(ctx context.Context, datID, machine string) ([]service.Entry, error) {
	rows, err := store.executor.QueryContext(
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
	entries := make([]service.Entry, 0)
	for rows.Next() {
		var name, status string
		var size int64
		var crc32Value, sha1Value, mergeName, biosName sql.NullString
		if err := rows.Scan(&name, &size, &crc32Value, &sha1Value, &status, &mergeName, &biosName); err != nil {
			return nil, fmt.Errorf("datindex/replace: %w", err)
		}
		entries = append(entries, service.Entry{
			BIOSName: dbexec.StringPointer(biosName), CRC32: dbexec.StringPointer(crc32Value),
			MergeName: dbexec.StringPointer(
				mergeName,
			), Name: name, SHA1: dbexec.StringPointer(
				sha1Value,
			), SizeBytes: size, Status: status,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("datindex/replace: %w", err)
	}
	return entries, nil
}

func (store Records) UpsertRequirement(ctx context.Context, input service.Requirement) error {
	_, err := recordstore.CreateBiosRequirements(ctx, store.executor, `
INSERT INTO bios_requirements(
 id,core_id,provider_id,target_id,source_kind,dat_machine_name,
 logical_name,requirement_mode,condition_code,activation_options_json,catalog_digest,
 size_bytes,md5,sha1,sha256,source_url,source_version,enabled,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,'DAT_MACHINE',?,?,'REQUIRED','ARCADE_DAT_DEPENDENCY',NULL,?,
 NULL,NULL,NULL,NULL,?,?,1,1,?,?)
ON CONFLICT(provider_id,target_id,logical_name) DO UPDATE SET
 dat_machine_name=excluded.dat_machine_name,requirement_mode=excluded.requirement_mode,
 condition_code=excluded.condition_code,catalog_digest=excluded.catalog_digest,
 source_url=excluded.source_url,source_version=excluded.source_version,enabled=1,
 version=CASE WHEN bios_requirements.catalog_digest!=excluded.catalog_digest
   OR bios_requirements.enabled=0 THEN bios_requirements.version+1 ELSE bios_requirements.version END,
 updated_at_ms=excluded.updated_at_ms
`, input.ID, input.CoreID, input.ProviderID, input.TargetID, input.Machine, input.LogicalName, input.Digest,
		input.SourceURL, input.VersionID, input.AtMS, input.AtMS)
	if err != nil {
		return fmt.Errorf("datindex/replace: %w", err)
	}
	return nil
}

func (store Records) DisableStale(ctx context.Context, input service.Retirement) error {
	_, err := recordstore.UpdateBiosRequirements(ctx, store.executor, recordstore.Update{
		Set: `
enabled=0,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
provider_id=? AND target_id=?
AND source_kind='DAT_MACHINE'
AND enabled=1
AND source_version!=?
`,
			Args: []any{input.ProviderID, input.TargetID, input.CurrentVersionID},
		},
		Values: []any{input.AtMS},
	})
	if err != nil {
		return fmt.Errorf("disable stale DAT BIOS requirements: %w", err)
	}
	return nil
}
