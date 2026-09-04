package dependencies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func bootstrapDAT(
	ctx context.Context,
	transaction *sql.Tx,
	target runtimeTarget,
	coreID string,
	relativePath string,
	digest string,
	machineCount int64,
	romCount int64,
	diskCount int64,
	biosSetCount int64,
	defaultBIOSCount int64,
	explicitBIOSCount int64,
	baseTargetCount int64,
	unresolvedCount int64,
	selectedForNewBindings bool,
	now time.Time,
) error {
	expected := catalogStats{
		machineCount: machineCount, romEntryCount: romCount, diskEntryCount: diskCount,
		biosSetCount: biosSetCount, defaultBIOSSetCount: defaultBIOSCount,
		explicitBIOSMachineCount: explicitBIOSCount, baseDependencyTargetCount: baseTargetCount,
		unresolvedCloneofCount: unresolvedCount,
	}
	datID, err := findOrCreateDATVersionID(ctx, transaction, target, digest)
	if err != nil {
		return err
	}
	if err := upsertBuiltInDAT(ctx, transaction, datID, coreID, target, relativePath, digest, now); err != nil {
		return err
	}
	parseStatus, selectedActive, indexed, err := inspectBuiltInDAT(ctx, transaction, datID)
	if err != nil {
		return err
	}
	if parseStatus == "READY" && indexed != expected {
		if err := repairBuiltInDATIndex(ctx, transaction, datID, selectedActive, now); err != nil {
			return err
		}
	}
	if !selectedForNewBindings {
		return nil
	}
	return retireSupersededBuiltInDAT(ctx, transaction, target, datID, now)
}

func findOrCreateDATVersionID(
	ctx context.Context,
	transaction *sql.Tx,
	target runtimeTarget, digest string,
) (string, error) {
	var id string
	err := transaction.QueryRowContext(
		ctx,
		`SELECT id FROM dat_versions
WHERE provider_id=? AND target_id=? AND sha256=? AND parser_version='retrom-dat-v1'`,
		target.providerID,
		target.targetID,
		digest,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		generated, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			return "", fmt.Errorf("generate DAT version id: %w", uuidErr)
		}
		id = generated.String()
	} else if err != nil {
		return "", fmt.Errorf("find DAT version: %w", err)
	}
	return id, nil
}

func upsertBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	id, coreID string, target runtimeTarget, relativePath, digest string,
	now time.Time,
) error {
	_, err := transaction.ExecContext(ctx, `
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
		id, coreID, target.providerID, target.targetID, relativePath, digest,
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert builtin DAT version: %w", err)
	}
	return nil
}

func inspectBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	datID string,
) (string, bool, catalogStats, error) {
	var parseStatus string
	var selectedActive int
	var indexedMachineCount, indexedROMCount, indexedDiskCount, indexedBIOSCount int64
	var indexedDefaultBIOSCount, indexedExplicitBIOSCount, indexedBaseTargetCount, indexedUnresolvedCount int64
	if err := transaction.QueryRowContext(ctx, `
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
`, datID).Scan(
		&parseStatus, &selectedActive, &indexedMachineCount, &indexedROMCount, &indexedDiskCount, &indexedBIOSCount,
		&indexedDefaultBIOSCount, &indexedExplicitBIOSCount, &indexedBaseTargetCount, &indexedUnresolvedCount,
	); err != nil {
		return "", false, catalogStats{}, fmt.Errorf("inspect built-in DAT index: %w", err)
	}
	indexed := catalogStats{
		machineCount: indexedMachineCount, romEntryCount: indexedROMCount, diskEntryCount: indexedDiskCount,
		biosSetCount: indexedBIOSCount, defaultBIOSSetCount: indexedDefaultBIOSCount,
		explicitBIOSMachineCount: indexedExplicitBIOSCount, baseDependencyTargetCount: indexedBaseTargetCount,
		unresolvedCloneofCount: indexedUnresolvedCount,
	}
	return parseStatus, selectedActive == 1, indexed, nil
}

func repairBuiltInDATIndex(
	ctx context.Context,
	transaction *sql.Tx,
	datID string,
	wasActive bool,
	now time.Time,
) error {
	if _, err := transaction.ExecContext(ctx, `
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
`, now.UnixMilli(), datID); err != nil {
		return fmt.Errorf("repair incomplete built-in DAT index: %w", err)
	}
	_ = wasActive
	return nil
}

func retireSupersededBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	target runtimeTarget, selectedDATID string,
	now time.Time,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE provider_id=? AND target_id=? AND id<>? AND is_active=1
`, now.UnixMilli(), target.providerID, target.targetID, selectedDATID)
	if err != nil {
		return fmt.Errorf("retire superseded built-in DAT: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count superseded built-in DAT rows: %w", err)
	}
	_ = changed
	return nil
}
