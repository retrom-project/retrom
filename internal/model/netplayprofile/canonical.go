package netplayprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type CanonicalProfileInput struct {
	ManifestProfile
	BundleSHA256           string
	SourceManifestDigest   string
	DependencySnapshotJSON string
}

func (registry *Registry) CanonicalProfile(input CanonicalProfileInput) ([]byte, string, error) {
	if _, ok := registry.Profile(input.ID); !ok || !ValidDigest(input.BundleSHA256) ||
		!ValidDigest(input.SourceManifestDigest) {
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
