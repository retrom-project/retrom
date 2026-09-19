package netplayprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	runtimecontract "retrom/internal/model/runtimecontract"
)

func ParseRegistry(contents []byte, bindings []runtimecontract.Binding) (*Registry, error) {
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
		if !validManifestProfile(profile, bindings) {
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
