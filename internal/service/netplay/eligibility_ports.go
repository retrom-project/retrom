package netplay

import (
	"fmt"

	"retrom/internal/transport/netplay/profile"
)

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

type Eligibility struct {
	repository EligibilityRepository
	registry   *profile.Registry
	tags       TagReader
	bios       BIOSResolver
}

func NewEligibility(
	repository EligibilityRepository, registry *profile.Registry, tags TagReader, bios BIOSResolver,
) *Eligibility {
	return &Eligibility{repository: repository, registry: registry, tags: tags, bios: bios}
}
