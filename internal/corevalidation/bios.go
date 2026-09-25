package corevalidation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"retrom/internal/contentcapability"
)

const SnapshotSchemaVersion = 1

const (
	SnapshotKindStatic = "STATIC"
	SnapshotKindArcade = "ARCADE"
)

const (
	MultiDiscContentKind   = "MULTI_DISC"
	MultiDiscParserVersion = "RETROM_MULTIDISC_M3U_V1"
	MultiDiscDelivery      = contentcapability.DeliveryEagerExternal
)

var ErrInvalidSnapshot = errors.New("CORE_VALIDATION_SNAPSHOT_INVALID")

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
	Kind          string             `json:"kind"`
	BIOS          []BIOSDependency   `json:"bios"`
	MultiDisc     *MultiDiscSnapshot `json:"multiDisc,omitempty"`
}

type arcadeRuntimeSnapshot struct {
	SchemaVersion     int               `json:"schemaVersion"`
	Kind              string            `json:"kind"`
	Machine           string            `json:"machine"`
	DATVersionID      string            `json:"datVersionId"`
	Closure           []json.RawMessage `json:"closure"`
	Dependencies      []json.RawMessage `json:"dependencies"`
	MissingEntries    []string          `json:"missingEntries"`
	MismatchedEntries []string          `json:"mismatchedEntries"`
	Warnings          []string          `json:"warnings"`
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

// ParseRuntimeBIOSDependencies dispatches current snapshots by semantic kind.
// Arcade closure files are frozen in the variant, not external static BIOS.
func ParseRuntimeBIOSDependencies(raw string) ([]BIOSDependency, error) {
	var envelope struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.SchemaVersion != SnapshotSchemaVersion {
		return nil, ErrInvalidSnapshot
	}
	switch envelope.Kind {
	case SnapshotKindStatic:
		snapshot, err := ParseSnapshot(raw)
		if err != nil {
			return nil, err
		}
		return snapshot.BIOS, nil
	case SnapshotKindArcade:
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
	return snapshot.SchemaVersion == SnapshotSchemaVersion && snapshot.Kind == SnapshotKindArcade &&
		snapshot.Machine != "" && snapshot.DATVersionID != "" &&
		snapshot.Closure != nil && snapshot.Dependencies != nil && snapshot.MissingEntries != nil &&
		snapshot.MismatchedEntries != nil && snapshot.Warnings != nil
}

func validSnapshot(snapshot Snapshot) bool {
	if snapshot.SchemaVersion != SnapshotSchemaVersion || snapshot.Kind != SnapshotKindStatic || snapshot.BIOS == nil {
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
	providerID, targetID, gameID string,
	datID *string,
	snapshot Snapshot,
) (string, error) {
	snapshotJSON, err := snapshot.JSON()
	if err != nil {
		return "", fmt.Errorf("corevalidation/digest: %w", err)
	}
	biosDigest := sha256.Sum256(snapshotJSON)
	input, err := json.Marshal(map[string]any{
		"biosDependencyDigest": hex.EncodeToString(biosDigest[:]),
		"providerId":           providerID,
		"targetId":             targetID,
		"datVersionId":         datID,
		"gameId":               gameID,
		"schemaVersion":        1,
	})
	if err != nil {
		return "", fmt.Errorf("corevalidation/digest input: %w", err)
	}
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:]), nil
}

const MultiDiscValidationSchema = 1

type MultiDiscValidationInput struct {
	GameVariantID           string
	GameID                  string
	ContentKind             string
	ProviderID              string
	TargetID                string
	ContentPolicySHA256     string
	DATVersionID            *string
	BIOSDependencySHA256    string
	OrderedDiscSHA256       []string
	CanonicalPlaylistSHA256 string
}

func BIOSDependencyDigest(snapshot Snapshot) (string, error) {
	encoded, err := json.Marshal(Snapshot{
		SchemaVersion: SnapshotSchemaVersion, Kind: SnapshotKindStatic, BIOS: snapshot.BIOS,
	})
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
		SchemaVersion           int      `json:"schemaVersion"`
		GameVariantID           string   `json:"gameVariantId"`
		GameID                  string   `json:"gameId"`
		ContentKind             string   `json:"contentKind"`
		ProviderID              string   `json:"providerId"`
		TargetID                string   `json:"targetId"`
		ContentPolicySHA256     string   `json:"contentPolicySha256"`
		DATVersionID            *string  `json:"datVersionId"`
		BIOSDependencySHA256    string   `json:"biosDependencySha256"`
		OrderedDiscSHA256       []string `json:"orderedDiscSha256"`
		CanonicalPlaylistSHA256 string   `json:"canonicalPlaylistSha256"`
	}{
		SchemaVersion: MultiDiscValidationSchema,
		GameVariantID: input.GameVariantID, GameID: input.GameID,
		ContentKind: input.ContentKind, ProviderID: input.ProviderID, TargetID: input.TargetID,
		ContentPolicySHA256:     input.ContentPolicySHA256,
		DATVersionID:            input.DATVersionID,
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
	if input.GameVariantID == "" || input.GameID == "" || input.ContentKind == "" ||
		input.ProviderID == "" || input.TargetID == "" || !validSHA256(input.ContentPolicySHA256) ||
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

var amigaComputerContentExtensions = map[string]bool{
	".adf": true, ".adz": true, ".dms": true, ".fdi": true, ".ipf": true,
	".raw": true, ".hdf": true, ".hdz": true, ".lha": true,
}

func BIOSApplies(condition, contentName string) bool {
	extension := strings.ToLower(path.Ext(contentName))
	switch condition {
	case "PCE_CD_CONTENT":
		return extension == ".chd"
	case "SEGA_CD_CONTENT":
		return extension == ".chd"
	case "AMIGA_CD32_CONTENT":
		return extension == ".chd" || extension == ".iso" || extension == ".nrg"
	case "AMIGA_COMPUTER_CONTENT":
		return amigaComputerContentExtensions[extension]
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

// BIOSInstallationUsable keeps catalog findings advisory after a safe upload.
func BIOSInstallationUsable(status string) bool {
	return status == "MATCHED" || status == "HASH_WARNING" || status == "MISSING_ENTRY"
}
