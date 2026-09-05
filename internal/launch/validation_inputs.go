package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/contentcapability"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
)

type validationInputs struct {
	GameID                string                   `json:"gameId"`
	GameVariantID         string                   `json:"gameVariantId"`
	GameVersion           int64                    `json:"gameVersion"`
	SourceManifestDigest  string                   `json:"sourceManifestDigest"`
	ProviderID            string                   `json:"providerId"`
	TargetID              string                   `json:"targetId"`
	ContentPolicy         contentcapability.Policy `json:"contentPolicy"`
	DATVersionID          any                      `json:"datVersionId"`
	ValidationInputDigest string                   `json:"validationInputDigest"`
	BIOSDependencyDigest  string                   `json:"biosDependencyDigest"`
}

type validationSnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Scope         validationScope  `json:"scope"`
	ExecutionID   string           `json:"executionId"`
	Inputs        validationInputs `json:"inputs"`
}

type validationScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func validationDedupeKey(variantID, digest string) string {
	canonical, _ := json.Marshal(map[string]string{"gameVariantId": variantID, "validationInputDigest": digest})
	value := sha256.New()
	_, _ = value.Write([]byte("retrom-job-dedupe-v1\x00VARIANT_VALIDATE\x00"))
	_, _ = value.Write(canonical)
	return hex.EncodeToString(value.Sum(nil))
}

type variantArcadeBIOSRow struct {
	logicalName         string
	dependencyState     string
	requirementID       sql.NullString
	requirementVersion  sql.NullInt64
	catalogDigest       sql.NullString
	requirementMode     sql.NullString
	condition           sql.NullString
	deliveryKind        sql.NullString
	emulatorPath        sql.NullString
	optionsJSON         sql.NullString
	installationID      sql.NullString
	installationVersion sql.NullInt64
	blobID              sql.NullString
	installationStatus  sql.NullString
}

func scanVariantArcadeBIOSRow(rows *sql.Rows) (variantArcadeBIOSRow, error) {
	var row variantArcadeBIOSRow
	if err := rows.Scan(
		&row.logicalName,
		&row.dependencyState,
		&row.requirementID,
		&row.requirementVersion,
		&row.catalogDigest,
		&row.requirementMode,
		&row.condition,
		&row.deliveryKind,
		&row.emulatorPath,
		&row.optionsJSON,
		&row.installationID,
		&row.installationVersion,
		&row.blobID,
		&row.installationStatus,
	); err != nil {
		return variantArcadeBIOSRow{}, fmt.Errorf("scan Arcade BIOS dependency: %w", err)
	}
	return row, nil
}

func variantArcadeBIOSDependency(
	row variantArcadeBIOSRow,
) (corevalidation.BIOSDependency, bool, bool, error) {
	if row.dependencyState == "SATISFIED_BY_CONTENT" {
		return corevalidation.BIOSDependency{}, false, true, nil
	}
	if !row.requirementID.Valid || !row.requirementVersion.Valid || !row.catalogDigest.Valid ||
		!row.requirementMode.Valid || !row.deliveryKind.Valid {
		return corevalidation.BIOSDependency{}, false, false, nil
	}
	dependency := corevalidation.BIOSDependency{
		BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{
			RequirementID: row.requirementID.String, RequirementVersion: row.requirementVersion.Int64,
			CatalogDigest: row.catalogDigest.String, LogicalName: row.logicalName,
			RequirementMode: row.requirementMode.String, DeliveryKind: row.deliveryKind.String,
			ConditionCode: nullStringPointer(row.condition), EmulatorPath: nullStringPointer(row.emulatorPath),
		},
		ActivationOptions:   make(map[string]string),
		InstallationID:      nullStringPointer(row.installationID),
		InstallationVersion: nullInt64Pointer(row.installationVersion),
		BlobID:              nullStringPointer(row.blobID),
		InstallationStatus:  nullStringPointer(row.installationStatus),
	}
	if row.optionsJSON.Valid {
		if err := json.Unmarshal([]byte(row.optionsJSON.String), &dependency.ActivationOptions); err != nil {
			return corevalidation.BIOSDependency{}, false, false, corevalidation.ErrInvalidSnapshot
		}
	}
	validInstallation := row.blobID.Valid && row.installationStatus.Valid &&
		(row.installationStatus.String == "MATCHED" || row.installationStatus.String == "HASH_WARNING")
	return dependency, true, validInstallation, nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func (service *Service) resolveVariantBIOS(
	ctx context.Context,
	database corevalidation.Queryer,
	variantID, contentID, providerID, targetID, contentLogicalName string,
	datID sql.NullString,
) (corevalidation.Snapshot, string, string, error) {
	snapshot, status, code, err := corevalidation.ResolveBIOS(
		ctx, database, providerID, targetID, contentLogicalName,
	)
	if err != nil {
		return snapshot, status, code, fmt.Errorf("launch validation static BIOS: %w", err)
	}
	if !datID.Valid {
		return snapshot, status, code, nil
	}
	rows, err := database.QueryContext(ctx, `
SELECT dependency.logical_archive,
dependency.state,
requirement.id,
requirement.version,
requirement.catalog_digest,
requirement.requirement_mode,
requirement.condition_code,
requirement.delivery_kind,
requirement.emulator_path,
requirement.activation_options_json,
installation.id,
installation.version,
installation.blob_id,
installation.status
FROM game_variants variant
JOIN variant_dependencies dependency ON dependency.game_variant_id=variant.id
AND dependency.kind='BIOS_OR_BASE'
LEFT JOIN bios_requirements requirement ON requirement.provider_id=? AND requirement.target_id=?
AND requirement.source_kind='DAT_MACHINE'
AND requirement.source_version=?
AND requirement.logical_name=dependency.logical_archive
AND requirement.enabled=1
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
AND installation.is_active=1
AND installation.validated_requirement_version=requirement.version
WHERE variant.id=? AND variant.game_id=? AND variant.dat_version_id IS ?
ORDER BY dependency.logical_archive
`, providerID, targetID, datID.String, variantID, contentID, nullableSQL(datID))
	if err != nil {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
			fmt.Errorf("launch validation Arcade BIOS: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		row, err := scanVariantArcadeBIOSRow(rows)
		if err != nil {
			return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
				fmt.Errorf("launch validation Arcade BIOS: %w", err)
		}
		dependency, include, validInstallation, err := variantArcadeBIOSDependency(row)
		if err != nil {
			return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
				fmt.Errorf("launch validation Arcade BIOS dependency: %w", err)
		}
		if !validInstallation {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		if include {
			snapshot.BIOS = append(snapshot.BIOS, dependency)
		}
	}
	if err := rows.Err(); err != nil {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE",
			fmt.Errorf("launch validation Arcade BIOS: %w", err)
	}
	return snapshot, status, code, nil
}

func (service *Service) validationDigests(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, contentID, contentLogicalName, contentKind, providerID, targetID string,
	contentPolicy contentcapability.Policy,
	datID sql.NullString,
) (string, string, error) {
	biosSnapshot, _, _, err := service.resolveVariantBIOS(
		ctx, transaction, variantID, contentID, providerID, targetID, contentLogicalName, datID,
	)
	if err != nil {
		return "", "", ErrBlocked
	}
	if contentKind == corevalidation.MultiDiscContentKind {
		digest, biosDigest, _, digestErr := service.multiDiscRevalidationInputs(
			ctx, transaction, variantID, contentID, providerID, targetID,
			contentPolicy, datID, biosSnapshot,
		)
		return digest, biosDigest, digestErr
	}
	digest, err := corevalidation.ProviderValidationInputDigest(
		providerID, targetID, contentID, datID, biosSnapshot,
	)
	if err != nil {
		return "", "", fmt.Errorf("launch validation digest: %w", err)
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return "", "", fmt.Errorf("launch validation snapshot: %w", err)
	}
	biosSnapshotDigest := sha256.Sum256(biosSnapshotJSON)
	return digest, hex.EncodeToString(biosSnapshotDigest[:]), nil
}

func (service *Service) currentValidationEvidence(
	ctx context.Context,
	variantID, contentID, contentLogicalName, contentKind, providerID, targetID string,
	contentPolicy contentcapability.Policy,
	datID sql.NullString,
) (string, string, corevalidation.Snapshot, string, string, error) {
	biosSnapshot, biosStatus, biosCode, err := service.resolveVariantBIOS(
		ctx, service.database, variantID, contentID, providerID, targetID, contentLogicalName, datID,
	)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("launch validation BIOS: %w", err)
	}
	if contentKind == corevalidation.MultiDiscContentKind {
		digest, biosDigest, snapshot, digestErr := service.multiDiscRevalidationInputs(
			ctx, service.database, variantID, contentID, providerID, targetID,
			contentPolicy, datID, biosSnapshot,
		)
		return digest, biosDigest, snapshot, biosStatus, biosCode, digestErr
	}
	digest, err := corevalidation.ProviderValidationInputDigest(
		providerID, targetID, contentID, datID, biosSnapshot,
	)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("launch validation digest: %w", err)
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("launch validation snapshot: %w", err)
	}
	biosDigest := sha256.Sum256(biosSnapshotJSON)
	return digest, hex.EncodeToString(biosDigest[:]), biosSnapshot, biosStatus, biosCode, nil
}

type arcadeSnapshotIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Machine       string `json:"machine"`
	DATVersionID  string `json:"datVersionId"`
}

func validateLockedArcadeSnapshot(raw, contentLogicalName, datID string) error {
	var identity arcadeSnapshotIdentity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return corevalidation.ErrInvalidSnapshot
	}
	machine := strings.TrimSuffix(filepath.Base(contentLogicalName), filepath.Ext(contentLogicalName))
	if identity.SchemaVersion != corevalidation.SnapshotSchemaVersion ||
		identity.Kind != corevalidation.SnapshotKindArcade ||
		identity.Machine != machine || identity.DATVersionID != datID {
		return corevalidation.ErrInvalidSnapshot
	}
	if _, err := corevalidation.ParseRuntimeBIOSDependencies(raw); err != nil {
		return fmt.Errorf("parse Arcade runtime dependency snapshot: %w", err)
	}
	return nil
}

func (service *Service) lockedArcadeDependencySnapshot(
	ctx context.Context,
	variantID, contentID, contentLogicalName, datID string,
) (string, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT variant.dependency_snapshot_json
FROM game_variants variant
WHERE variant.id=? AND variant.game_id=? AND variant.dat_version_id=?
`, variantID, contentID, datID).Scan(&raw); err != nil {
		return "", fmt.Errorf("load locked Arcade dependency snapshot: %w", err)
	}
	if err := validateLockedArcadeSnapshot(raw, contentLogicalName, datID); err != nil {
		return "", fmt.Errorf("validate locked Arcade dependency snapshot: %w", err)
	}
	return raw, nil
}
