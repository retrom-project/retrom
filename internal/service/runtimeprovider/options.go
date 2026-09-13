package runtimeprovider

import (
	"fmt"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimeoptions"
)

func validateHostOptionStrategies(catalog runtimecatalog.Catalog, providers []ProviderProjection) error {
	schemas := make(map[string]runtimebundle.TargetOptionsSchema)
	for _, provider := range providers {
		for _, target := range provider.Targets {
			schemas[provider.Active.ProviderID+"\x00"+target.Target.ID] = target.Target.TargetOptionsSchema
		}
	}
	for _, binding := range catalog.Bindings {
		strategy, registered := runtimecatalog.Strategy(binding.DetectorProfile)
		if !registered {
			return ErrProjectionInvalid
		}
		schema := schemas[binding.ProviderID+"\x00"+binding.TargetID]
		if err := runtimeoptions.ValidateSchema(strategy.Options, schema); err != nil {
			return fmt.Errorf("%w: options for %s/%s: %w", ErrProjectionInvalid, binding.ProviderID, binding.TargetID, err)
		}
	}
	return nil
}
