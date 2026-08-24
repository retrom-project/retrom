package netplay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
)

const (
	ProtocolVersion        = "retrom-netplay-v2"
	WebSocketSubprotocol   = "retrom.netplay.v1"
	PlayerAdapterID        = retromruntime.NetplayPlayerAdapterID
	NetplayAdapterID       = "ejs-netplay-4.2.3-v1"
	ControlCount           = 24
	CheckpointEveryFrames  = 120
	MaxPredictionFrames    = 8
	MaxRollbackFrames      = 120
	CanonicalHistoryFrames = 600
	MaxStateBytes          = 1_048_576
	ManifestRelativePath   = "netplay/v2/manifest.json"
	ManifestSchemaRelative = "netplay/v2/schema.json"
)

var ErrManifestInvalid = errors.New("NETPLAY_MANIFEST_INVALID")

type Protocol struct {
	Version                string   `json:"version"`
	PlayerAdapterID        string   `json:"playerAdapterId"`
	NetplayAdapterID       string   `json:"netplayAdapterId"`
	ControlCount           int      `json:"controlCount"`
	CheckpointEveryFrames  int      `json:"checkpointEveryFrames"`
	MaxPredictionFrames    int      `json:"maxPredictionFrames"`
	MaxRollbackFrames      int      `json:"maxRollbackFrames"`
	CanonicalHistoryFrames int      `json:"canonicalHistoryFrames"`
	MaxStateBytes          int      `json:"maxStateBytes"`
	AllowedContentKinds    []string `json:"allowedContentKinds"`
}

type ManifestProfile struct {
	ID                  string   `json:"id"`
	EmulatorJSVersion   string   `json:"emulatorjsVersion"`
	CoreID              string   `json:"coreId"`
	PlatformIDs         []string `json:"platformIds"`
	CoreArtifactSHA256  string   `json:"coreArtifactSha256"`
	MaxPlayers          int      `json:"maxPlayers"`
	MaxPredictionFrames int      `json:"maxPredictionFrames"`
}

type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Protocol      Protocol          `json:"protocol"`
	Profiles      []ManifestProfile `json:"profiles"`
}

type Registry struct {
	Manifest       Manifest
	ManifestDigest string
	profiles       map[string]ManifestProfile
}

func LoadRegistry(dependencyRoot string, dependencySet *dependencies.Set) (*Registry, error) {
	manifestPath := filepath.Join(dependencyRoot, ManifestRelativePath)
	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("%w: manifest unavailable", ErrManifestInvalid)
	}
	if _, err := os.Stat(filepath.Join(dependencyRoot, ManifestSchemaRelative)); err != nil {
		return nil, fmt.Errorf("%w: schema unavailable", ErrManifestInvalid)
	}
	return parseRegistry(contents, dependencySet)
}

func parseRegistry(contents []byte, dependencySet *dependencies.Set) (*Registry, error) {
	if dependencySet == nil {
		return nil, fmt.Errorf("%w: dependencies unavailable", ErrManifestInvalid)
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: schema", ErrManifestInvalid)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing data", ErrManifestInvalid)
	}
	if manifest.SchemaVersion != 4 || !validProtocol(manifest.Protocol) || len(manifest.Profiles) == 0 {
		return nil, fmt.Errorf("%w: protocol", ErrManifestInvalid)
	}
	version := dependencySet.Versions["4.2.3"]
	if version == nil || version.Manifest.EmulatorJS.PlayerAdapter.ID != retromruntime.SinglePlayerAdapter423ID {
		return nil, fmt.Errorf("%w: player adapter", ErrManifestInvalid)
	}
	cores := make(map[string]dependencies.SelectedCore, len(version.Manifest.EmulatorJS.SelectedCores))
	for _, core := range version.Manifest.EmulatorJS.SelectedCores {
		cores[core.CoreID] = core
	}
	profiles := make(map[string]ManifestProfile, len(manifest.Profiles))
	for _, profile := range manifest.Profiles {
		core, exists := cores[profile.CoreID]
		if !exists || !validManifestProfile(profile, core) {
			return nil, fmt.Errorf("%w: profile", ErrManifestInvalid)
		}
		if _, duplicate := profiles[profile.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate profile", ErrManifestInvalid)
		}
		profiles[profile.ID] = profile
	}
	digest := sha256.Sum256(contents)
	return &Registry{
		Manifest: manifest, ManifestDigest: hex.EncodeToString(digest[:]), profiles: profiles,
	}, nil
}

func validProtocol(protocol Protocol) bool {
	return protocol.Version == ProtocolVersion && protocol.PlayerAdapterID == PlayerAdapterID &&
		protocol.NetplayAdapterID == NetplayAdapterID && protocol.ControlCount == ControlCount &&
		protocol.CheckpointEveryFrames == CheckpointEveryFrames &&
		protocol.MaxPredictionFrames == MaxPredictionFrames &&
		protocol.MaxRollbackFrames == MaxRollbackFrames &&
		protocol.CanonicalHistoryFrames == CanonicalHistoryFrames && protocol.MaxStateBytes == MaxStateBytes &&
		slices.Equal(protocol.AllowedContentKinds, []string{"SINGLE_FILE"})
}

func validManifestProfile(profile ManifestProfile, core dependencies.SelectedCore) bool {
	return profile.ID != "" && profile.ID == strings.ToLower(profile.ID) && len(profile.ID) <= 64 &&
		validPlatformIDs(profile.PlatformIDs) &&
		profile.EmulatorJSVersion == "4.2.3" && profile.CoreArtifactSHA256 == core.SHA256 &&
		profile.MaxPlayers >= 2 && profile.MaxPlayers <= 4 &&
		profile.MaxPredictionFrames >= 0 && profile.MaxPredictionFrames <= MaxPredictionFrames &&
		slices.Contains(core.SupportedContentKinds, "SINGLE_FILE")
}

func validPlatformIDs(platformIDs []string) bool {
	if len(platformIDs) < 1 || len(platformIDs) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(platformIDs))
	for _, platformID := range platformIDs {
		if platformID == "" || len(platformID) > 64 || platformID != strings.ToLower(platformID) {
			return false
		}
		for _, character := range platformID {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
		if _, duplicate := seen[platformID]; duplicate {
			return false
		}
		seen[platformID] = struct{}{}
	}
	return true
}

func (registry *Registry) Profile(id string) (ManifestProfile, bool) {
	profile, ok := registry.profiles[id]
	return profile, ok
}

func (registry *Registry) Profiles() []ManifestProfile {
	result := slices.Clone(registry.Manifest.Profiles)
	slices.SortFunc(result, func(left, right ManifestProfile) int { return strings.Compare(left.ID, right.ID) })
	return result
}

func (registry *Registry) SupportsPlatformCoreArtifact(platformID, coreID, emulatorVersion, artifactSHA string) bool {
	if registry == nil {
		return false
	}
	for _, profile := range registry.Manifest.Profiles {
		if profile.CoreID == coreID && profile.EmulatorJSVersion == emulatorVersion &&
			profile.CoreArtifactSHA256 == artifactSHA && slices.Contains(profile.PlatformIDs, platformID) {
			return true
		}
	}
	return false
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type CanonicalProfileInput struct {
	ManifestProfile
	CoreArtifactID         string
	GameVariantRevisionID  string
	DependencySnapshotJSON string
	DefaultCoreOptions     map[string]string
}

func (registry *Registry) CanonicalProfile(input CanonicalProfileInput) ([]byte, string, error) {
	if _, ok := registry.Profile(input.ID); !ok || input.CoreArtifactID == "" ||
		input.GameVariantRevisionID == "" || input.DefaultCoreOptions == nil {
		return nil, "", ErrManifestInvalid
	}
	dependencyDigest := sha256.Sum256([]byte(input.DependencySnapshotJSON))
	value := map[string]any{
		"schemaVersion": 1, "protocolVersion": ProtocolVersion, "profileId": input.ID,
		"emulatorjsVersion": input.EmulatorJSVersion, "playerAdapterId": PlayerAdapterID,
		"platformIds":      input.PlatformIDs,
		"netplayAdapterId": NetplayAdapterID, "coreArtifactId": input.CoreArtifactID,
		"coreArtifactSha256":       input.CoreArtifactSHA256,
		"gameVariantRevisionId":    input.GameVariantRevisionID,
		"sourceManifestDigest":     registry.ManifestDigest,
		"dependencySnapshotDigest": hex.EncodeToString(dependencyDigest[:]),
		"defaultCoreOptions":       input.DefaultCoreOptions, "controlCount": ControlCount,
		"maxPlayers": input.MaxPlayers, "maxPredictionFrames": input.MaxPredictionFrames,
		"maxRollbackFrames": MaxRollbackFrames, "checkpointEveryFrames": CheckpointEveryFrames,
		"canonicalHistoryFrames": CanonicalHistoryFrames, "maxStateBytes": MaxStateBytes,
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize profile: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}
