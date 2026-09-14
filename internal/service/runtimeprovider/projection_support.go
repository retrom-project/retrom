package runtimeprovider

import (
	"fmt"

	"golang.org/x/mod/semver"

	"retrom/internal/capability/runtime/runtimebundle"
)

func validateProviderVersion(
	candidate runtimebundle.ActiveProvider,
	current CurrentProvider,
	exists bool,
) (bool, error) {
	if !exists {
		return true, nil
	}
	comparison := semver.Compare("v"+candidate.ProviderVersion, "v"+current.Version)
	if comparison < 0 {
		return false, fmt.Errorf("%w: %s", ErrProviderDowngrade, candidate.ProviderID)
	}
	if comparison == 0 && candidate.BundleSHA256 != current.BundleSHA256 {
		return false, fmt.Errorf("%w: %s", ErrProviderVersionRebuilt, candidate.ProviderID)
	}
	return comparison > 0 || candidate.BundleSHA256 != current.BundleSHA256, nil
}

func projectionInvalid(err error) error {
	return fmt.Errorf("%w: %w", ErrProjectionInvalid, err)
}
