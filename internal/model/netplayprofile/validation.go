package netplayprofile

import (
	"encoding/hex"
	"slices"
	"strings"

	runtimecontract "retrom/internal/model/runtimecontract"
)

func validProtocol(protocol Protocol) bool {
	return protocol.Version == ProtocolVersion && protocol.ControlCount == ControlCount &&
		protocol.CheckpointEveryFrames == CheckpointEveryFrames &&
		protocol.MaxPredictionFrames == MaxPredictionFrames && protocol.MaxRollbackFrames == MaxRollbackFrames &&
		protocol.CanonicalHistoryFrames == CanonicalHistoryFrames && protocol.MaxStateBytes == MaxStateBytes &&
		slices.Equal(protocol.AllowedContentKinds, []string{"SINGLE_FILE"})
}

func validManifestProfile(profile ManifestProfile, bindings []runtimecontract.Binding) bool {
	if !validManifestProfileShape(profile) {
		return false
	}
	for _, binding := range bindings {
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

func ValidDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
