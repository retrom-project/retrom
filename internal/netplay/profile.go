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
)

const (
	ProtocolVersion        = "retrom-netplay-v2"
	WebSocketSubprotocol   = "retrom.netplay.v1"
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
	ProviderID          string   `json:"providerId"`
	TargetID            string   `json:"targetId"`
	CoreID              string   `json:"coreId"`
	PlatformIDs         []string `json:"platformIds"`
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
	contents, err := os.ReadFile(filepath.Join(dependencyRoot, ManifestRelativePath))
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
	if manifest.SchemaVersion != 5 || !validProtocol(manifest.Protocol) || len(manifest.Profiles) == 0 {
		return nil, fmt.Errorf("%w: protocol", ErrManifestInvalid)
	}
	profiles := make(map[string]ManifestProfile, len(manifest.Profiles))
	for _, profile := range manifest.Profiles {
		if !validManifestProfile(profile, dependencySet) {
			return nil, fmt.Errorf("%w: profile", ErrManifestInvalid)
		}
		if _, duplicate := profiles[profile.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate profile", ErrManifestInvalid)
		}
		profiles[profile.ID] = profile
	}
	digest := sha256.Sum256(contents)
	return &Registry{Manifest: manifest, ManifestDigest: hex.EncodeToString(digest[:]), profiles: profiles}, nil
}

func validProtocol(protocol Protocol) bool {
	return protocol.Version == ProtocolVersion && protocol.ControlCount == ControlCount &&
		protocol.CheckpointEveryFrames == CheckpointEveryFrames &&
		protocol.MaxPredictionFrames == MaxPredictionFrames && protocol.MaxRollbackFrames == MaxRollbackFrames &&
		protocol.CanonicalHistoryFrames == CanonicalHistoryFrames && protocol.MaxStateBytes == MaxStateBytes &&
		slices.Equal(protocol.AllowedContentKinds, []string{"SINGLE_FILE"})
}

func validManifestProfile(profile ManifestProfile, dependencySet *dependencies.Set) bool {
	if !validManifestProfileShape(profile) {
		return false
	}
	for _, binding := range dependencySet.RuntimeCatalog.Bindings {
		if binding.ProviderID == profile.ProviderID && binding.TargetID == profile.TargetID &&
			binding.CoreID == profile.CoreID && binding.LaunchPolicy != "DISABLED" &&
			slices.Contains(binding.AcceptedContentKinds, "SINGLE_FILE") {
			return profilePlatformsBound(profile.PlatformIDs, binding.PlatformIDs)
		}
	}
	return false
}

func validManifestProfileShape(profile ManifestProfile) bool {
	return profile.ID != "" && profile.ID == strings.ToLower(profile.ID) && len(profile.ID) <= 64 &&
		profile.ProviderID != "" && profile.TargetID != "" && profile.CoreID != "" &&
		validPlatformIDs(profile.PlatformIDs) &&
		profile.MaxPlayers >= 2 && profile.MaxPlayers <= 4 && profile.MaxPredictionFrames >= 0 &&
		profile.MaxPredictionFrames <= MaxPredictionFrames
}

func profilePlatformsBound(profilePlatforms, bindingPlatforms []string) bool {
	return !slices.ContainsFunc(profilePlatforms, func(platformID string) bool {
		return !slices.Contains(bindingPlatforms, platformID)
	})
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

func (registry *Registry) SupportsPlatformTarget(
	platformID, coreID, providerID, targetID string,
) bool {
	if registry == nil {
		return false
	}
	return slices.ContainsFunc(registry.Manifest.Profiles, func(profile ManifestProfile) bool {
		return profile.CoreID == coreID && profile.ProviderID == providerID && profile.TargetID == targetID &&
			slices.Contains(profile.PlatformIDs, platformID)
	})
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
	BundleSHA256           string
	SourceManifestDigest   string
	DependencySnapshotJSON string
}

func (registry *Registry) CanonicalProfile(input CanonicalProfileInput) ([]byte, string, error) {
	if _, ok := registry.Profile(input.ID); !ok || !validDigest(input.BundleSHA256) ||
		!validDigest(input.SourceManifestDigest) {
		return nil, "", ErrManifestInvalid
	}
	dependencyDigest := sha256.Sum256([]byte(input.DependencySnapshotJSON))
	value := map[string]any{
		"schemaVersion": 2, "protocolVersion": ProtocolVersion, "profileId": input.ID,
		"coreId": input.CoreID, "platformIds": input.PlatformIDs,
		"providerId": input.ProviderID, "targetId": input.TargetID,
		"bundleSha256":             input.BundleSHA256,
		"sourceManifestDigest":     input.SourceManifestDigest,
		"dependencySnapshotDigest": hex.EncodeToString(dependencyDigest[:]),
		"controlCount":             ControlCount, "maxPlayers": input.MaxPlayers,
		"maxPredictionFrames": input.MaxPredictionFrames, "maxRollbackFrames": MaxRollbackFrames,
		"checkpointEveryFrames":  CheckpointEveryFrames,
		"canonicalHistoryFrames": CanonicalHistoryFrames, "maxStateBytes": MaxStateBytes,
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize profile: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}
