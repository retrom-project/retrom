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

const (
	MultiDiscContentKind   = "MULTI_DISC"
	MultiDiscParserVersion = "RETROM_MULTIDISC_M3U_V1"
	MultiDiscDelivery      = "EAGER_EXTERNAL_FILES"
)

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

type MultiDiscMissingEntry struct {
	Ordinal             int    `json:"ordinal"`
	SourceReference     string `json:"sourceReference"`
	NormalizedReference string `json:"normalizedReference"`
}

type MultiDiscSnapshot struct {
	ContentKind             string                  `json:"contentKind,omitempty"`
	ParserVersion           string                  `json:"parserVersion,omitempty"`
	DiscCount               int                     `json:"discCount"`
	MissingEntries          []MultiDiscMissingEntry `json:"missingEntries"`
	OrderedDiscSHA256       []string                `json:"orderedDiscSha256,omitempty"`
	CanonicalPlaylistSHA256 string                  `json:"canonicalPlaylistSha256,omitempty"`
	Delivery                string                  `json:"delivery,omitempty"`
}

type Snapshot struct {
	SchemaVersion int                `json:"schemaVersion"`
	BIOS          []BIOSDependency   `json:"bios"`
	MultiDisc     *MultiDiscSnapshot `json:"multiDisc,omitempty"`
}

type arcadeRuntimeSnapshot struct {
	SchemaVersion     int               `json:"schemaVersion"`
	Machine           string            `json:"machine"`
	DATVersionID      string            `json:"datVersionId"`
	Closure           []json.RawMessage `json:"closure"`
	Dependencies      []json.RawMessage `json:"dependencies"`
	MissingEntries    []string          `json:"missingEntries"`
	MismatchedEntries []string          `json:"mismatchedEntries"`
	Warnings          []string          `json:"warnings"`
}

func Catalog(ctx context.Context, database Queryer, providerID, targetID string) ([]BIOSCatalogEntry, error) {
	rows, err := database.QueryContext(ctx, `
SELECT id,version,catalog_digest,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path
FROM bios_requirements
WHERE provider_id=? AND target_id=? AND source_kind='STATIC' AND enabled=1
ORDER BY logical_name,id
`, providerID, targetID)
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
	providerID, targetID, contentLogicalName string,
) (Snapshot, string, string, error) {
	if providerID == "" || targetID == "" || contentLogicalName == "" {
		return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", ErrInvalidSnapshot
	}
	rows, err := database.QueryContext(ctx, `
SELECT q.id,q.version,q.catalog_digest,q.logical_name,q.requirement_mode,q.condition_code,
       q.delivery_kind,q.emulator_path,q.activation_options_json,
       i.id,i.version,i.blob_id,i.status
FROM bios_requirements q
LEFT JOIN bios_installations i ON i.requirement_id=q.id AND i.is_active=1
WHERE q.provider_id=? AND q.target_id=? AND q.source_kind='STATIC' AND q.enabled=1
ORDER BY q.logical_name,q.id
`, providerID, targetID)
	if err != nil {
		return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", fmt.Errorf("corevalidation/bios: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	snapshot := Snapshot{SchemaVersion: SnapshotSchemaVersion, BIOS: make([]BIOSDependency, 0)}
	status, code := "READY", "READY"
	for rows.Next() {
		dependency, applies, validInstallation, scanErr := scanBIOSDependency(rows, contentLogicalName)
		if scanErr != nil {
			return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", scanErr
		}
		if !applies {
			continue
		}
		if !validInstallation &&
			(dependency.RequirementMode != "OPTIONAL" || dependency.InstallationStatus != nil) {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		snapshot.BIOS = append(snapshot.BIOS, dependency)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", fmt.Errorf("corevalidation/bios: %w", err)
	}
	return snapshot, status, code, nil
}

func scanBIOSDependency(
	rows *sql.Rows,
	contentLogicalName string,
) (BIOSDependency, bool, bool, error) {
	var dependency BIOSDependency
	var condition, emulatorPath, optionsJSON sql.NullString
	var installationID, blobID, installationStatus sql.NullString
	var installationVersion sql.NullInt64
	if err := rows.Scan(
		&dependency.RequirementID, &dependency.RequirementVersion,
		&dependency.CatalogDigest, &dependency.LogicalName, &dependency.RequirementMode,
		&condition, &dependency.DeliveryKind, &emulatorPath, &optionsJSON,
		&installationID, &installationVersion, &blobID, &installationStatus,
	); err != nil {
		return BIOSDependency{}, false, false, fmt.Errorf("corevalidation/bios: %w", err)
	}
	if condition.Valid && !BIOSApplies(condition.String, contentLogicalName) {
		return BIOSDependency{}, false, false, nil
	}
	dependency.ConditionCode = nullableString(condition)
	dependency.EmulatorPath = nullableString(emulatorPath)
	dependency.ActivationOptions = map[string]string{}
	if optionsJSON.Valid {
		if err := json.Unmarshal([]byte(optionsJSON.String), &dependency.ActivationOptions); err != nil {
			return BIOSDependency{}, false, false, ErrInvalidSnapshot
		}
	}
	dependency.InstallationID = nullableString(installationID)
	dependency.InstallationVersion = nullableInt64(installationVersion)
	dependency.BlobID = nullableString(blobID)
	dependency.InstallationStatus = nullableString(installationStatus)
	validInstallation := installationStatus.Valid && blobID.Valid &&
		(installationStatus.String == "MATCHED" || installationStatus.String == "HASH_WARNING")
	return dependency, true, validInstallation, nil
}

func (snapshot Snapshot) JSON() ([]byte, error) {
	if !validSnapshot(snapshot) {
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
	if err := decoder.Decode(&snapshot); err != nil || !validSnapshot(snapshot) {
		return Snapshot{}, ErrInvalidSnapshot
	}
	return snapshot, nil
}

// ParseRuntimeBIOSDependencies accepts both dependency snapshot families that
// can back a published variant. Static BIOS and multi-disc revisions use the
// schema-v1 snapshot. Arcade revisions use a schema-v2 DAT closure whose
// parent and BIOS files are already frozen as variant files, so it contributes
// no external static BIOS dependencies at launch time.
func ParseRuntimeBIOSDependencies(raw string) ([]BIOSDependency, error) {
	var envelope struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil, ErrInvalidSnapshot
	}
	switch envelope.SchemaVersion {
	case SnapshotSchemaVersion:
		snapshot, err := ParseSnapshot(raw)
		if err != nil {
			return nil, err
		}
		return snapshot.BIOS, nil
	case 2:
		var snapshot arcadeRuntimeSnapshot
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&snapshot); err != nil || !validArcadeRuntimeSnapshot(snapshot) {
			return nil, ErrInvalidSnapshot
		}
		return []BIOSDependency{}, nil
	default:
		return nil, ErrInvalidSnapshot
	}
}

func validArcadeRuntimeSnapshot(snapshot arcadeRuntimeSnapshot) bool {
	return snapshot.SchemaVersion == 2 && snapshot.Machine != "" && snapshot.DATVersionID != "" &&
		snapshot.Closure != nil && snapshot.Dependencies != nil && snapshot.MissingEntries != nil &&
		snapshot.MismatchedEntries != nil && snapshot.Warnings != nil
}

func validSnapshot(snapshot Snapshot) bool {
	if snapshot.SchemaVersion != SnapshotSchemaVersion || snapshot.BIOS == nil {
		return false
	}
	if snapshot.MultiDisc == nil {
		return true
	}
	return validMultiDiscSnapshot(*snapshot.MultiDisc)
}

func validMultiDiscSnapshot(snapshot MultiDiscSnapshot) bool {
	if snapshot.DiscCount < 2 || snapshot.DiscCount > 8 || snapshot.MissingEntries == nil ||
		len(snapshot.MissingEntries) > snapshot.DiscCount {
		return false
	}
	if !validMultiDiscMissingEntries(snapshot) {
		return false
	}
	if len(snapshot.MissingEntries) > 0 {
		return true
	}
	return validCompleteMultiDiscSnapshot(snapshot)
}

func validMultiDiscMissingEntries(snapshot MultiDiscSnapshot) bool {
	seen := make(map[int]struct{}, len(snapshot.MissingEntries))
	for _, entry := range snapshot.MissingEntries {
		if entry.Ordinal < 0 || entry.Ordinal >= snapshot.DiscCount ||
			entry.SourceReference == "" || entry.NormalizedReference == "" {
			return false
		}
		if _, duplicate := seen[entry.Ordinal]; duplicate {
			return false
		}
		seen[entry.Ordinal] = struct{}{}
	}
	return true
}

func validCompleteMultiDiscSnapshot(snapshot MultiDiscSnapshot) bool {
	if snapshot.ContentKind != MultiDiscContentKind || snapshot.ParserVersion != MultiDiscParserVersion ||
		snapshot.Delivery != MultiDiscDelivery || len(snapshot.OrderedDiscSHA256) != snapshot.DiscCount ||
		!validSHA256(snapshot.CanonicalPlaylistSHA256) {
		return false
	}
	for _, digest := range snapshot.OrderedDiscSHA256 {
		if !validSHA256(digest) {
			return false
		}
	}
	return true
}

func ProviderValidationInputDigest(
	providerID, targetID, targetContractSHA256, gameCompatibilityLine, contentID string,
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
		"providerId":            providerID,
		"targetId":              targetID,
		"targetContractSha256":  targetContractSHA256,
		"gameCompatibilityLine": gameCompatibilityLine,
		"datVersionId":          nullableSQLString(datID),
		"gameContentRevisionId": contentID,
		"schemaVersion":         3,
	})
	if err != nil {
		return "", fmt.Errorf("corevalidation/digest input: %w", err)
	}
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:]), nil
}

const MultiDiscValidationSchema = "RETROM_VARIANT_VALIDATION_INPUT_V3"

type MultiDiscValidationInput struct {
	GameVariantID           string
	GameContentRevisionID   string
	ContentKind             string
	ProviderID              string
	TargetID                string
	TargetContractSHA256    string
	GameCompatibilityLine   string
	ContentPolicySHA256     string
	DATVersionID            sql.NullString
	BIOSDependencySHA256    string
	OrderedDiscSHA256       []string
	CanonicalPlaylistSHA256 string
}

func ContentPolicyDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func BIOSDependencyDigest(snapshot Snapshot) (string, error) {
	encoded, err := json.Marshal(Snapshot{SchemaVersion: SnapshotSchemaVersion, BIOS: snapshot.BIOS})
	if err != nil {
		return "", fmt.Errorf("corevalidation/bios digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func MultiDiscValidationInputDigest(input MultiDiscValidationInput) (string, error) {
	if !validMultiDiscValidationInput(input) {
		return "", fmt.Errorf("corevalidation/multi-disc digest: %w", ErrInvalidSnapshot)
	}
	ordered := make([]string, len(input.OrderedDiscSHA256))
	copy(ordered, input.OrderedDiscSHA256)
	canonical, err := json.Marshal(struct {
		SchemaVersion           string   `json:"schemaVersion"`
		GameVariantID           string   `json:"gameVariantId"`
		GameContentRevisionID   string   `json:"gameContentRevisionId"`
		ContentKind             string   `json:"contentKind"`
		ProviderID              string   `json:"providerId"`
		TargetID                string   `json:"targetId"`
		TargetContractSHA256    string   `json:"targetContractSha256"`
		GameCompatibilityLine   string   `json:"gameCompatibilityLine"`
		ContentPolicySHA256     string   `json:"contentPolicySha256"`
		DATVersionID            any      `json:"datVersionId"`
		BIOSDependencySHA256    string   `json:"biosDependencySha256"`
		OrderedDiscSHA256       []string `json:"orderedDiscSha256"`
		CanonicalPlaylistSHA256 string   `json:"canonicalPlaylistSha256"`
	}{
		SchemaVersion: MultiDiscValidationSchema,
		GameVariantID: input.GameVariantID, GameContentRevisionID: input.GameContentRevisionID,
		ContentKind: input.ContentKind, ProviderID: input.ProviderID, TargetID: input.TargetID,
		TargetContractSHA256:    input.TargetContractSHA256,
		GameCompatibilityLine:   input.GameCompatibilityLine,
		ContentPolicySHA256:     input.ContentPolicySHA256,
		DATVersionID:            nullableSQLString(input.DATVersionID),
		BIOSDependencySHA256:    input.BIOSDependencySHA256,
		OrderedDiscSHA256:       ordered,
		CanonicalPlaylistSHA256: input.CanonicalPlaylistSHA256,
	})
	if err != nil {
		return "", fmt.Errorf("corevalidation/multi-disc digest input: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func validMultiDiscValidationInput(input MultiDiscValidationInput) bool {
	if input.GameVariantID == "" || input.GameContentRevisionID == "" || input.ContentKind == "" ||
		input.ProviderID == "" || input.TargetID == "" || !validSHA256(input.TargetContractSHA256) ||
		input.GameCompatibilityLine == "" || !validSHA256(input.ContentPolicySHA256) ||
		!validSHA256(input.BIOSDependencySHA256) || !validSHA256(input.CanonicalPlaylistSHA256) ||
		len(input.OrderedDiscSHA256) < 2 || len(input.OrderedDiscSHA256) > 8 {
		return false
	}
	for _, value := range input.OrderedDiscSHA256 {
		if !validSHA256(value) {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
