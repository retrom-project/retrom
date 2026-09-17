package netplay

import (
	"fmt"
	model "retrom/internal/model/netplay"

	"retrom/internal/transport/netplay/profile"
)

func freezeRoomProfile(
	registry *profile.Registry,
	gameID string,
	candidate model.EligibleProfile,
) (model.FrozenRoomProfile, error) {
	canonical, digest, err := registry.CanonicalProfile(profile.CanonicalProfileInput{
		ManifestProfile:        candidate.Manifest,
		BundleSHA256:           candidate.BundleSHA256,
		SourceManifestDigest:   candidate.SourceManifestDigest,
		DependencySnapshotJSON: candidate.DependencySnapshotJSON,
	})
	if err != nil {
		return model.FrozenRoomProfile{}, fmt.Errorf("%w: %w", model.ErrInvalidProfile, err)
	}
	return model.FrozenRoomProfile{
		Selection: model.RoomSelection{
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
