package runtimeprovider

import (
	"fmt"

	"golang.org/x/mod/semver"
)

// ValidateProviderVersionChange checks whether a provider version change is
// acceptable. Returns (changed bool, err error). A downgrade or same-version
// rebuild is rejected.
func ValidateProviderVersionChange(
	candidateVersion, candidateBundleSHA256 string,
	currentVersion, currentBundleSHA256 string,
	providerID string,
	exists bool,
) (bool, error) {
	if !exists {
		return true, nil
	}
	comparison := semver.Compare("v"+candidateVersion, "v"+currentVersion)
	if comparison < 0 {
		return false, fmt.Errorf("%w: %s", ErrProviderDowngrade, providerID)
	}
	if comparison == 0 && candidateBundleSHA256 != currentBundleSHA256 {
		return false, fmt.Errorf(
			"%w: %s", ErrProviderVersionRebuilt, providerID,
		)
	}
	return comparison > 0 || candidateBundleSHA256 != currentBundleSHA256, nil
}

// ValidateCheckpointFormats checks that all existing checkpoint formats are
// readable by the new target's checkpoint configuration.
func ValidateCheckpointFormats(
	providerID, targetID string,
	readFormats []string,
	existingFormats []string,
) error {
	readable := make(map[string]bool, len(readFormats))
	for _, format := range readFormats {
		readable[format] = true
	}
	for _, format := range existingFormats {
		if !readable[format] {
			return fmt.Errorf(
				"%w: %s/%s %s",
				ErrProviderCheckpointUnreadable,
				providerID, targetID, format,
			)
		}
	}
	return nil
}
