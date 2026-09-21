package netplay

import (
	"fmt"

	"retrom/internal/netplay/profile"
)

type FrozenRoomProfile struct {
	Selection                          RoomSelection
	ProviderID, TargetID, BundleSHA256 string
	Canonical                          []byte
}

func freezeRoomProfile(
	registry *profile.Registry,
	gameID string,
	candidate EligibleProfile,
) (FrozenRoomProfile, error) {
	canonical, digest, err := registry.CanonicalProfile(profile.CanonicalProfileInput{
		ManifestProfile:        candidate.Manifest,
		BundleSHA256:           candidate.BundleSHA256,
		SourceManifestDigest:   candidate.SourceManifestDigest,
		DependencySnapshotJSON: candidate.DependencySnapshotJSON,
	})
	if err != nil {
		return FrozenRoomProfile{}, fmt.Errorf("%w: %w", ErrInvalidProfile, err)
	}
	return FrozenRoomProfile{
		Selection: RoomSelection{
			GameID:     gameID,
			VariantID:  candidate.VariantID,
			ProfileID:  candidate.Manifest.ID,
			Digest:     digest,
			MaxPlayers: candidate.Manifest.MaxPlayers,
		},
		ProviderID:   candidate.Manifest.ProviderID,
		TargetID:     candidate.Manifest.TargetID,
		BundleSHA256: candidate.BundleSHA256,
		Canonical:    canonical,
	}, nil
}
