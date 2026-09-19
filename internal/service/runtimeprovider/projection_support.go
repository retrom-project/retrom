package runtimeprovider

import (
	"fmt"

	"golang.org/x/mod/semver"

	runtimecontract "retrom/internal/model/runtimecontract"
	model "retrom/internal/model/runtimeprovider"
)

func validateProviderVersion(
	candidate runtimecontract.ActiveProvider,
	current model.CurrentProvider,
	exists bool,
) (bool, error) {
	if !exists {
		return true, nil
	}
	comparison := semver.Compare("v"+candidate.ProviderVersion, "v"+current.Version)
	if comparison < 0 {
		return false, fmt.Errorf("%w: %s", model.ErrProviderDowngrade, candidate.ProviderID)
	}
	if comparison == 0 && candidate.BundleSHA256 != current.BundleSHA256 {
		return false, fmt.Errorf("%w: %s", model.ErrProviderVersionRebuilt, candidate.ProviderID)
	}
	return comparison > 0 || candidate.BundleSHA256 != current.BundleSHA256, nil
}

func projectionInvalid(err error) error {
	return fmt.Errorf("%w: %w", model.ErrProjectionInvalid, err)
}
