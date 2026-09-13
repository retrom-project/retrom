package dependencies

import (
	"context"
	"fmt"

	"retrom/internal/repo/datindex"
	service "retrom/internal/service/dependencies"
)

func (records catalogRecords) Version(ctx context.Context, id string) (int64, error) {
	var version int64
	if err := records.transaction.QueryRowContext(ctx, `
SELECT version
FROM dat_versions
WHERE id=?
`, id).Scan(&version); err != nil {
		return 0, fmt.Errorf("dependencies/read DAT version: %w", err)
	}
	return version, nil
}

func (records catalogRecords) Publish(ctx context.Context, input service.CatalogPublication) error {
	if input.Replace {
		if err := datindex.Replace(ctx, records.transaction, input.DATID, input.Catalog); err != nil {
			return fmt.Errorf("dependencies/write DAT index: %w", err)
		}
	}
	stats := input.Catalog.Stats
	if _, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE dat_versions
SET parse_status='READY',
is_active=0,
machine_count=?,
rom_entry_count=?,
disk_entry_count=?,
bios_set_count=?,
default_bios_set_count=?,
explicit_bios_machine_count=?,
base_dependency_target_count=?,
unresolved_relation_count=?,
parsed_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`,
		stats.MachineCount,
		stats.ROMEntryCount,
		stats.DiskEntryCount,

		stats.BIOSSetCount,
		stats.DefaultBIOSSetCount,
		stats.ExplicitBIOSMachineCount,
		stats.BaseDependencyTargetCount,

		stats.UnresolvedCloneofTargetCount+stats.UnresolvedRomofTargetCount,
		input.AtMS,
		input.AtMS,
		input.DATID,
	); err != nil {
		return fmt.Errorf("dependencies/publish DAT statistics: %w", err)
	}
	return nil
}

func (records catalogRecords) MarkParsing(ctx context.Context, id string, now int64) error {
	if _, err := records.transaction.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='PARSING',
version=version+1,
updated_at_ms=?
WHERE id=?
AND parse_status IN ('PENDING',
'PARSING')
`, now, id); err != nil {
		return fmt.Errorf("dependencies/mark DAT parsing: %w", err)
	}
	return nil
}

func (records catalogRecords) MarkFailed(ctx context.Context, id string, now int64) error {
	if _, err := records.transaction.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='FAILED',
is_active=0,
machine_count=NULL,
rom_entry_count=NULL,
disk_entry_count=NULL,
bios_set_count=NULL,
default_bios_set_count=NULL,
explicit_bios_machine_count=NULL,
base_dependency_target_count=NULL,
unresolved_relation_count=NULL,
parsed_at_ms=NULL,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now, id); err != nil {
		return fmt.Errorf("dependencies/mark DAT failure: %w", err)
	}
	return nil
}
