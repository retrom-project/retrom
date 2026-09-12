package dependencies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/recordstore"
	service "retrom/internal/service/dependencies"

	"github.com/google/uuid"
)

func (records biosRecords) Upsert(ctx context.Context, input service.BIOSRequirement) error {
	_, err := recordstore.CreateBiosRequirements(ctx, records.executor, `
INSERT INTO bios_requirements(id,
core_id,
provider_id,
target_id,
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
updated_at_ms,
delivery_kind,
emulator_path,archive_members_json) VALUES(?,
?,
?,
?,
'STATIC',
NULL,
?,
?,
?,
?,
?,
?,
?,
NULL,
?,
?,
?,
1,
1,
?,
?,
?,
?,?) ON CONFLICT(provider_id,target_id,
logical_name)
DO UPDATE SET requirement_mode=excluded.requirement_mode,
condition_code=excluded.condition_code,
activation_options_json=excluded.activation_options_json,
catalog_digest=excluded.catalog_digest,
size_bytes=excluded.size_bytes,
md5=excluded.md5,
sha256=excluded.sha256,
source_url=excluded.source_url,
source_version=excluded.source_version,
delivery_kind=excluded.delivery_kind,
emulator_path=excluded.emulator_path,
archive_members_json=excluded.archive_members_json,
enabled=1,
version=CASE WHEN bios_requirements.catalog_digest!=excluded.catalog_digest
  THEN bios_requirements.version+1 ELSE bios_requirements.version END,
updated_at_ms=excluded.updated_at_ms
`,
		input.ID, input.CoreID, input.ProviderID, input.TargetID, input.LogicalName,
		input.Mode, input.ConditionCode, input.Options,
		input.Digest, input.SizeBytes, input.MD5, input.SHA256, input.SourceURL, input.VersionName, input.AtMS, input.AtMS,
		input.Delivery, input.EmulatorPath, input.ArchiveMembers)
	if err != nil {
		return fmt.Errorf("dependencies/upsert BIOS requirement: %w", err)
	}
	return nil
}

func (records datRecords) Register(ctx context.Context, input service.DATRegistration) (service.RegisteredDAT, error) {
	var id string
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT id FROM dat_versions
WHERE provider_id=? AND target_id=? AND sha256=? AND parser_version='retrom-dat-v1'`,
		input.Target.ProviderID,
		input.Target.TargetID,
		input.SHA256,
	).Scan(
		&id,
	)
	if errors.Is(err, sql.ErrNoRows) {
		generated, err := uuid.NewV7()
		if err != nil {
			return service.RegisteredDAT{}, fmt.Errorf("dependencies/create DAT ID: %w", err)
		}
		id = generated.String()
	} else if err != nil {
		return service.RegisteredDAT{}, fmt.Errorf("dependencies/find DAT ID: %w", err)
	}
	if _, err := recordstore.CreateDatVersions(
		ctx,
		records.executor,
		`
INSERT INTO dat_versions(id,
 core_id,
 provider_id,
 target_id,
 builtin_relative_path,
 sha256,
 parser_version,
 parse_status,
 is_active,
 machine_count,
 rom_entry_count,
 disk_entry_count,
 bios_set_count,
 default_bios_set_count,
 explicit_bios_machine_count,
 base_dependency_target_count,
 unresolved_relation_count,
 version,
 created_at_ms,
 updated_at_ms,
 parsed_at_ms,
 activated_at_ms)
VALUES(?,
?,
?,
?,
?,
?,
'retrom-dat-v1',
'PENDING',
0,
NULL,
NULL,
NULL,
NULL,
NULL,
NULL,
NULL,
NULL,
1,
?,
?,
NULL,
NULL)
ON CONFLICT(provider_id,target_id,
 sha256,
parser_version) DO UPDATE SET
  builtin_relative_path=excluded.builtin_relative_path,
updated_at_ms=excluded.updated_at_ms
`,

		id,
		input.CoreID,
		input.Target.ProviderID,
		input.Target.TargetID,
		input.RelativePath,
		input.SHA256,
		input.AtMS,
		input.AtMS,
	); err != nil {
		return service.RegisteredDAT{}, fmt.Errorf("dependencies/upsert DAT version: %w", err)
	}
	return records.registered(ctx, id)
}

func (records datRecords) registered(ctx context.Context, id string) (service.RegisteredDAT, error) {
	result := service.RegisteredDAT{ID: id}
	var active int
	stats := &result.Stats
	err := records.executor.QueryRowContext(ctx, `
SELECT d.parse_status,
d.is_active,
COALESCE(d.machine_count,
-1),
COALESCE(d.rom_entry_count,
-1),
COALESCE(d.disk_entry_count,
-1),
COALESCE(d.bios_set_count,
-1),
COALESCE(d.default_bios_set_count,
-1),
COALESCE(d.explicit_bios_machine_count,
-1),
COALESCE(d.base_dependency_target_count,
-1),
COALESCE(d.unresolved_relation_count,
-1)
FROM dat_versions d
WHERE d.id=?
`, id).Scan(&result.ParseStatus, &active,
		&stats.MachineCount, &stats.ROMEntryCount, &stats.DiskEntryCount, &stats.BIOSSetCount, &stats.DefaultBIOSSetCount,
		&stats.ExplicitBIOSMachineCount, &stats.BaseDependencyTargetCount, &stats.UnresolvedCloneofCount)
	if err != nil {
		return service.RegisteredDAT{}, fmt.Errorf("dependencies/read registered DAT: %w", err)
	}
	return result, nil
}

func (records datRecords) Reset(ctx context.Context, id string, now int64) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='PENDING',
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
activated_at_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, id); err != nil {
		return fmt.Errorf("dependencies/reset DAT index: %w", err)
	}
	return nil
}

func (records datRecords) Retire(ctx context.Context, target service.RuntimeTarget, id string, now int64) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE provider_id=? AND target_id=? AND id<>? AND is_active=1
`, now, target.ProviderID, target.TargetID, id); err != nil {
		return fmt.Errorf("dependencies/retire DAT index: %w", err)
	}
	return nil
}
