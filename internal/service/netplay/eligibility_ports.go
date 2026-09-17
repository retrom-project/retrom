package netplay

import (
	"fmt"

	model "retrom/internal/model/netplay"

	"retrom/internal/transport/netplay/profile"
)

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

type Eligibility struct {
	repository model.EligibilityRepository
	registry   *profile.Registry
	tags       model.TagReader
	bios       model.BIOSResolver
}

func NewEligibility(
	repository model.EligibilityRepository, registry *profile.Registry, tags model.TagReader, bios model.BIOSResolver,
) *Eligibility {
	return &Eligibility{repository: repository, registry: registry, tags: tags, bios: bios}
}
