package corevalidation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"retrom/internal/cleanup"
)

const SnapshotSchemaVersion = 1

var ErrInvalidSnapshot = errors.New("CORE_VALIDATION_SNAPSHOT_INVALID")

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type BIOSCatalogEntry struct {
	RequirementID      string  `json:"requirementId"`
	RequirementVersion int64   `json:"requirementVersion"`
	CatalogDigest      string  `json:"catalogDigest"`
	LogicalName        string  `json:"logicalName"`
	RequirementMode    string  `json:"requirementMode"`
	ConditionCode      *string `json:"conditionCode"`
	DeliveryKind       string  `json:"deliveryKind"`
	EmulatorPath       *string `json:"emulatorPath"`
}

type BIOSDependency struct {
	BIOSCatalogEntry
	ActivationOptions   map[string]string `json:"activationOptions"`
	InstallationID      *string           `json:"installationId"`
	InstallationVersion *int64            `json:"installationVersion"`
	BlobID              *string           `json:"blobId"`
	InstallationStatus  *string           `json:"installationStatus"`
}

type Snapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	BIOS          []BIOSDependency `json:"bios"`
}

func Catalog(ctx context.Context, database Queryer, artifactID string) ([]BIOSCatalogEntry, error) {
	rows, err := database.QueryContext(ctx, `
SELECT id,version,catalog_digest,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path
FROM bios_requirements
WHERE core_artifact_id=? AND source_kind='STATIC' AND enabled=1
ORDER BY logical_name,id
`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("corevalidation/catalog: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]BIOSCatalogEntry, 0)
	for rows.Next() {
		var entry BIOSCatalogEntry
		var condition, emulatorPath sql.NullString
		if err := rows.Scan(
			&entry.RequirementID,
			&entry.RequirementVersion,
			&entry.CatalogDigest,
			&entry.LogicalName,
			&entry.RequirementMode,
			&condition,
			&entry.DeliveryKind,
			&emulatorPath,
		); err != nil {
			return nil, fmt.Errorf("corevalidation/catalog: %w", err)
		}
		entry.ConditionCode = nullableString(condition)
		entry.EmulatorPath = nullableString(emulatorPath)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("corevalidation/catalog: %w", err)
	}
	return entries, nil
}

// ResolveBIOS returns the exact applicable static BIOS dependency set. Its
// ordered JSON is suitable for immutable revision evidence and digest input.
func ResolveBIOS(
	ctx context.Context,
	database Queryer,
	artifactID, contentLogicalName string,
) (Snapshot, string, string, error) {
	rows, err := database.QueryContext(ctx, `
SELECT q.id,q.version,q.catalog_digest,q.logical_name,q.requirement_mode,q.condition_code,
       q.delivery_kind,q.emulator_path,q.activation_options_json,
       i.id,i.version,i.blob_id,i.status
FROM bios_requirements q
LEFT JOIN bios_installations i ON i.requirement_id=q.id AND i.is_active=1
WHERE q.core_artifact_id=? AND q.source_kind='STATIC' AND q.enabled=1
ORDER BY q.logical_name,q.id
`, artifactID)
	if err != nil {
		return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", fmt.Errorf("corevalidation/bios: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	snapshot := Snapshot{SchemaVersion: SnapshotSchemaVersion, BIOS: make([]BIOSDependency, 0)}
	status, code := "READY", "READY"
	for rows.Next() {
		var dependency BIOSDependency
		var condition, emulatorPath, optionsJSON sql.NullString
		var installationID, blobID, installationStatus sql.NullString
		var installationVersion sql.NullInt64
		if err := rows.Scan(
			&dependency.RequirementID,
			&dependency.RequirementVersion,
			&dependency.CatalogDigest,
			&dependency.LogicalName,
			&dependency.RequirementMode,
			&condition,
			&dependency.DeliveryKind,
			&emulatorPath,
			&optionsJSON,
			&installationID,
			&installationVersion,
			&blobID,
			&installationStatus,
		); err != nil {
			return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", fmt.Errorf("corevalidation/bios: %w", err)
		}
		if condition.Valid && !BIOSApplies(condition.String, contentLogicalName) {
			continue
		}
		dependency.ConditionCode = nullableString(condition)
		dependency.EmulatorPath = nullableString(emulatorPath)
		dependency.ActivationOptions = map[string]string{}
		if optionsJSON.Valid {
			if err := json.Unmarshal([]byte(optionsJSON.String), &dependency.ActivationOptions); err != nil {
				return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", ErrInvalidSnapshot
			}
		}
		dependency.InstallationID = nullableString(installationID)
		dependency.InstallationVersion = nullableInt64(installationVersion)
		dependency.BlobID = nullableString(blobID)
		dependency.InstallationStatus = nullableString(installationStatus)
		validInstallation := installationStatus.Valid &&
			(installationStatus.String == "MATCHED" || installationStatus.String == "HASH_WARNING") && blobID.Valid
		if !validInstallation && (dependency.RequirementMode != "OPTIONAL" || installationStatus.Valid) {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		snapshot.BIOS = append(snapshot.BIOS, dependency)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", fmt.Errorf("corevalidation/bios: %w", err)
	}
	return snapshot, status, code, nil
}

func (snapshot Snapshot) JSON() ([]byte, error) {
	if snapshot.SchemaVersion != SnapshotSchemaVersion || snapshot.BIOS == nil {
		return nil, ErrInvalidSnapshot
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("corevalidation/snapshot: %w", err)
	}
	return encoded, nil
}

func ParseSnapshot(raw string) (Snapshot, error) {
	var snapshot Snapshot
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.SchemaVersion != SnapshotSchemaVersion ||
		snapshot.BIOS == nil {
		return Snapshot{}, ErrInvalidSnapshot
	}
	return snapshot, nil
}

func ValidationInputDigest(
	artifactID, contentID string,
	datID sql.NullString,
	snapshot Snapshot,
) (string, error) {
	snapshotJSON, err := snapshot.JSON()
	if err != nil {
		return "", fmt.Errorf("corevalidation/digest: %w", err)
	}
	biosDigest := sha256.Sum256(snapshotJSON)
	input, err := json.Marshal(map[string]any{
		"biosDependencyDigest":  hex.EncodeToString(biosDigest[:]),
		"coreArtifactId":        artifactID,
		"datVersionId":          nullableSQLString(datID),
		"gameContentRevisionId": contentID,
		"schemaVersion":         2,
	})
	if err != nil {
		return "", fmt.Errorf("corevalidation/digest input: %w", err)
	}
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:]), nil
}

func BIOSApplies(condition, contentName string) bool {
	extension := strings.ToLower(path.Ext(contentName))
	switch condition {
	case "FDS_CONTENT":
		return extension == ".fds"
	case "GB_CONTENT":
		return extension == ".gb" || extension == ".dmg"
	case "GBC_CONTENT":
		return extension == ".gbc"
	case "GBA_CONTENT":
		return extension == ".gba"
	case "GAME_GENIE_ADDON_MODE", "MGBA_SGB_MODEL":
		return false
	default:
		return true
	}
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copyValue := value.String
	return &copyValue
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copyValue := value.Int64
	return &copyValue
}

func nullableSQLString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
