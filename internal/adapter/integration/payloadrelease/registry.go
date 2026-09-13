package payloadrelease

import (
	"fmt"

	repository "retrom/internal/repo/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

type (
	Ownership     = application.Ownership
	OwnershipEdge = application.OwnershipEdge
)

func OwnershipRegistry() []OwnershipEdge { return application.OwnershipRegistry() }

func ValidateOwnershipRegistry() error {
	edges, err := repository.LoadLifecycleBlobEdges()
	if err != nil {
		return fmt.Errorf("load payload ownership registry: %w", err)
	}
	if err := application.ValidateOwnershipRegistry(edges); err != nil {
		return fmt.Errorf("validate payload ownership registry: %w", err)
	}
	return nil
}
