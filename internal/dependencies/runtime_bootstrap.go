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
	artifactID string,
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
	if !selectedForNewBindings {
		return nil
	}
	return retireSupersededBuiltInDAT(ctx, transaction, artifactID, datID, now)
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

func bootstrapCore(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	version *Version,
	selectedForNewBindings bool,
	index int,
	core SelectedCore,
	component struct {
		Repository, SourceCommit, Association string
	},
	now time.Time,
) (string, error) {
	compatibility := coreCompatibility(core, version.Manifest.SchemaVersion)
	association := sourceAssociation(component.Association)
	provenance := map[string]any{
		"schemaVersion": 1, "dependencyManifestSha256": version.ManifestSHA256,
		"artifactFlavor":          core.ArtifactFlavor,
		"coreBundleVersion":       core.BundleVersion,
		"manifestEntryPointer":    fmt.Sprintf("/emulatorjs/selected_core_artifacts/%d", index),
		"sourceAssociationStatus": association,
		"sourceUrl":               component.Repository + "/tree/" + component.SourceCommit,
		"notes":                   []string{},
	}
	compatibilityJSON, _ := json.Marshal(compatibility)
	provenanceJSON, _ := json.Marshal(provenance)
	return persistBootstrappedCore(
		ctx, transaction, versionName, version, selectedForNewBindings, core, now,
		compatibilityJSON, provenanceJSON,
	)
}

func coreCompatibility(core SelectedCore, _ int) map[string]any {
	result := map[string]any{
		"schemaVersion": 5, "runtimeCoreId": core.RuntimeCoreID,
		"adapterAbi":                core.AdapterABI,
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

// Artifact insertion remains contiguous so every immutable payload field is auditable.
func persistBootstrappedCore(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	version *Version,
	selectedForNewBindings bool,
	core SelectedCore,
	now time.Time,
	compatibilityJSON, provenanceJSON []byte,
) (string, error) {
	path := core.LocalPath
	if core.PathInRelease != nil {
		path = *core.PathInRelease
	}
	var id string
	err := transaction.QueryRowContext(ctx, `
SELECT id FROM core_artifacts
WHERE core_id=? AND route_key='DEFAULT' AND artifact_set_sha256=?
`, core.CoreID, core.ArtifactSetSHA256).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		generated, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			return "", fmt.Errorf("generate core artifact id: %w", uuidErr)
		}
		id = generated.String()
	} else if err != nil {
		return "", fmt.Errorf("find core artifact: %w", err)
	}
	selected := 0
	if selectedForNewBindings {
		selected = 1
	}
	// Deselect first so the partial unique index permits a current-artifact switch.
	if selected == 1 {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE core_artifacts
SET selected_for_new_bindings=0,version=version+1,updated_at_ms=?
WHERE core_id=? AND route_key='DEFAULT' AND selected_for_new_bindings=1 AND id<>?`,
			now.UnixMilli(),
			core.CoreID,
			id,
		); err != nil {
			return "", fmt.Errorf("deselect previous core artifact: %w", err)
		}
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO core_artifacts(id,
 core_id,
 route_key,
 runtime_family,
 runtime_adapter_kind,
 runtime_version,
 adapter_id,
 entry_path,
 size_bytes,
 sha256,
 manifest_sha256,
 artifact_set_sha256,
 requires_threads,
 save_payload_kind,
 save_max_bytes,
 provenance_json,
 compatibility_json,
 selected_for_new_bindings,
 available_for_launch,
 version,
 created_at_ms,
 updated_at_ms)
VALUES(?,
?,
'DEFAULT',
'EMULATORJS',
'EMULATORJS',
?,
?,
?,
?,
?,
?,
?,
?,
'RUNTIME_STATE',
67108864,
?,
?,
?,
1,
1,
?,
?)
ON CONFLICT(core_id,route_key,artifact_set_sha256) DO NOTHING
`,
		id,
		core.CoreID,
		versionName,
		version.Manifest.EmulatorJS.PlayerAdapter.ID,
		path,
		core.SizeBytes,
		core.SHA256,
		version.ManifestSHA256,
		core.ArtifactSetSHA256,
		boolToInteger(core.Threads),
		string(provenanceJSON),
		string(compatibilityJSON),
		selected,
		now.UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		return "", fmt.Errorf("insert core artifact: %w", err)
	}
	if err := transaction.QueryRowContext(ctx, `
SELECT id FROM core_artifacts
WHERE core_id=? AND route_key='DEFAULT' AND artifact_set_sha256=?
AND runtime_family='EMULATORJS' AND runtime_adapter_kind='EMULATORJS'
AND runtime_version=? AND adapter_id=? AND entry_path=? AND size_bytes=? AND sha256=?
AND manifest_sha256=? AND requires_threads=? AND save_payload_kind='RUNTIME_STATE'
AND save_max_bytes=67108864 AND provenance_json=? AND compatibility_json=?
AND available_for_launch=1
`, core.CoreID, core.ArtifactSetSHA256, versionName, version.Manifest.EmulatorJS.PlayerAdapter.ID,
		path, core.SizeBytes, core.SHA256, version.ManifestSHA256, boolToInteger(core.Threads),
		string(provenanceJSON), string(compatibilityJSON)).Scan(&id); err != nil {
		return "", fmt.Errorf("verify immutable core artifact: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts
SET selected_for_new_bindings=?,version=version+1,updated_at_ms=?
WHERE id=? AND selected_for_new_bindings<>?
`, selected, now.UnixMilli(), id, selected); err != nil {
		return "", fmt.Errorf("select core artifact: %w", err)
	}
	return id, nil
}

func boolToInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
