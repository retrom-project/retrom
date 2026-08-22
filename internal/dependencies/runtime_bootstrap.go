package dependencies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func bootstrapDAT(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
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
	activeVersion bool,
	now time.Time,
) error {
	expected := catalogStats{
		machineCount: machineCount, romEntryCount: romCount, diskEntryCount: diskCount,
		biosSetCount: biosSetCount, defaultBIOSSetCount: defaultBIOSCount,
		explicitBIOSMachineCount: explicitBIOSCount, baseDependencyTargetCount: baseTargetCount,
		unresolvedCloneofCount: unresolvedCount,
	}
	artifactID, err := findDATArtifact(ctx, transaction, versionName, coreID, activeVersion)
	if err != nil {
		return err
	}
	datID, err := findOrCreateDATVersionID(ctx, transaction, artifactID, digest)
	if err != nil {
		return err
	}
	if err := upsertBuiltInDAT(ctx, transaction, datID, coreID, artifactID, relativePath, digest, now); err != nil {
		return err
	}
	parseStatus, selectedActive, indexed, err := inspectBuiltInDAT(ctx, transaction, datID)
	if err != nil {
		return err
	}
	if parseStatus == "READY" && indexed != expected {
		if err := repairBuiltInDATIndex(ctx, transaction, datID, artifactID, selectedActive, now); err != nil {
			return err
		}
	}
	if !activeVersion {
		return nil
	}
	return retireSupersededBuiltInDAT(ctx, transaction, artifactID, datID, now)
}

func findDATArtifact(
	ctx context.Context,
	transaction *sql.Tx,
	versionName, coreID string,
	activeVersion bool,
) (string, error) {
	var artifactID string
	if err := transaction.QueryRowContext(ctx,
		"SELECT id FROM core_artifacts WHERE core_id = ? AND emulatorjs_version = ? AND enabled = ?",
		coreID, versionName, boolToInteger(activeVersion)).Scan(&artifactID); err != nil {
		return "", fmt.Errorf("find DAT core artifact: %w", err)
	}
	return artifactID, nil
}

func findOrCreateDATVersionID(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, digest string,
) (string, error) {
	var id string
	err := transaction.QueryRowContext(
		ctx,
		`SELECT id FROM dat_versions
WHERE core_artifact_id = ? AND sha256 = ? AND parser_version = 'retrom-dat-v1'`,
		artifactID,
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
	id, coreID, artifactID, relativePath, digest string,
	now time.Time,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO dat_versions(id,
 core_id,
 core_artifact_id,
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
ON CONFLICT(core_artifact_id,
 sha256,
parser_version) DO UPDATE SET
  builtin_relative_path=excluded.builtin_relative_path,
updated_at_ms=excluded.updated_at_ms
`,
		id, coreID, artifactID, relativePath, digest, now.UnixMilli(), now.UnixMilli())
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
	datID, artifactID string,
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
	if !wasActive {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, now.UnixMilli(), artifactID); err != nil {
		return fmt.Errorf("advance artifact for repaired active DAT: %w", err)
	}
	return nil
}

func retireSupersededBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, selectedDATID string,
	now time.Time,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE core_artifact_id=? AND id<>? AND is_active=1
`, now.UnixMilli(), artifactID, selectedDATID)
	if err != nil {
		return fmt.Errorf("retire superseded built-in DAT: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count superseded built-in DAT rows: %w", err)
	}
	if changed == 0 {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, now.UnixMilli(), artifactID); err != nil {
		return fmt.Errorf("advance DAT-selected artifact: %w", err)
	}
	return nil
}

func boolToInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func bootstrapCore(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	version *Version,
	activeVersion bool,
	index int,
	core SelectedCore,
	component struct {
		Repository, SourceCommit, Association string
	},
	now time.Time,
) error {
	compatibility := coreCompatibility(core, version.Manifest.SchemaVersion)
	association := sourceAssociation(component.Association)
	provenance := map[string]any{
		"schemaVersion": 1, "dependencyManifestSha256": version.ManifestSHA256,
		"manifestEntryPointer":    fmt.Sprintf("/emulatorjs/selected_core_artifacts/%d", index),
		"sourceAssociationStatus": association,
		"sourceUrl":               component.Repository + "/tree/" + component.SourceCommit,
		"notes":                   []string{},
	}
	compatibilityJSON, _ := json.Marshal(compatibility)
	provenanceJSON, _ := json.Marshal(provenance)
	return persistBootstrappedCore(
		ctx, transaction, versionName, activeVersion, core, now,
		compatibilityJSON, provenanceJSON, nullableCommit(association, component.SourceCommit),
	)
}

func coreCompatibility(core SelectedCore, _ int) map[string]any {
	result := map[string]any{
		"schemaVersion": 5, "runtimeCoreId": core.RuntimeCoreID,
		"requestedArtifactBasename": core.RequestedArtifactBasename,
		"canvasResizePolicy":        core.CanvasResizePolicy,
		"defaultOptions":            core.DefaultOptions,
		"inputMode":                 core.InputMode,
		"startupActions":            core.StartupActions,
		"supportedContentKinds":     core.SupportedContentKinds,
		"multiDisc":                 nil,
	}
	if core.MultiDisc != nil {
		result["multiDisc"] = map[string]any{
			"maxDiscs":      core.MultiDisc.MaxDiscs,
			"maxTotalBytes": core.MultiDisc.MaxTotalBytes,
			"delivery":      core.MultiDisc.Delivery,
		}
	}
	return result
}

func sourceAssociation(value string) string {
	association := "INFERRED_BUILD_TIME"
	if value == "EMBEDDED_GIT_VERSION" || value == "EXACT_COMMIT" || value == "EXACT_RELEASE" {
		association = "EXACT_COMMIT"
	}
	return association
}

// The upsert remains contiguous so every persisted artifact field is auditable.
func persistBootstrappedCore(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	activeVersion bool,
	core SelectedCore,
	now time.Time,
	compatibilityJSON, provenanceJSON []byte,
	sourceCommit any,
) error {
	path := core.LocalPath
	if core.PathInRelease != nil {
		path = *core.PathInRelease
	}
	var id string
	err := transaction.QueryRowContext(ctx,
		"SELECT id FROM core_artifacts WHERE core_id = ? AND emulatorjs_version = ? AND sha256 = ?",
		core.CoreID, versionName, core.SHA256).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		generated, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			return fmt.Errorf("generate core artifact id: %w", uuidErr)
		}
		id = generated.String()
	} else if err != nil {
		return fmt.Errorf("find core artifact: %w", err)
	}
	active := 0
	if activeVersion {
		active = 1
	}
	// Disable first so the partial unique index permits an active-version switch.
	if active == 1 {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE core_artifacts SET enabled = 0, version = version + 1, updated_at_ms = ?
WHERE core_id = ? AND enabled = 1 AND id != ?`,
			now.UnixMilli(),
			core.CoreID,
			id,
		); err != nil {
			return fmt.Errorf("disable previous core artifact: %w", err)
		}
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO core_artifacts(id,
 core_id,
 emulatorjs_version,
 bundle_version,
 flavor,
 relative_path,
 size_bytes,
 sha256,
 source_commit,
 provenance_json,
 compatibility_config_json,
 enabled,
 version,
 created_at_ms,
 updated_at_ms)
VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
1,
?,
?)
ON CONFLICT(core_id,
 emulatorjs_version,
 sha256) DO UPDATE SET
  bundle_version=excluded.bundle_version,
 flavor=excluded.flavor,
 relative_path=excluded.relative_path,
  size_bytes=excluded.size_bytes,
 source_commit=excluded.source_commit,
 provenance_json=excluded.provenance_json,
  compatibility_config_json=excluded.compatibility_config_json,
 enabled=excluded.enabled,
 version=core_artifacts.version + CASE WHEN
  core_artifacts.bundle_version IS NOT excluded.bundle_version OR
  core_artifacts.flavor IS NOT excluded.flavor OR
  core_artifacts.relative_path IS NOT excluded.relative_path OR
  core_artifacts.size_bytes IS NOT excluded.size_bytes OR
  core_artifacts.compatibility_config_json IS NOT excluded.compatibility_config_json OR
  core_artifacts.enabled IS NOT excluded.enabled
 THEN 1 ELSE 0 END,
  updated_at_ms=excluded.updated_at_ms
WHERE core_artifacts.bundle_version IS NOT excluded.bundle_version
 OR core_artifacts.flavor IS NOT excluded.flavor
 OR core_artifacts.relative_path IS NOT excluded.relative_path
 OR core_artifacts.size_bytes IS NOT excluded.size_bytes
 OR core_artifacts.source_commit IS NOT excluded.source_commit
 OR core_artifacts.provenance_json IS NOT excluded.provenance_json
 OR core_artifacts.compatibility_config_json IS NOT excluded.compatibility_config_json
 OR core_artifacts.enabled IS NOT excluded.enabled
`,
		id,
		core.CoreID,
		versionName,
		core.BundleVersion,
		core.ArtifactFlavor,
		path,
		core.SizeBytes,
		core.SHA256,
		sourceCommit,
		string(provenanceJSON),
		string(compatibilityJSON),
		active,
		now.UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("upsert core artifact: %w", err)
	}
	return nil
}

func nullableCommit(association, commit string) any {
	if association == "EXACT_COMMIT" {
		return commit
	}
	return nil
}
