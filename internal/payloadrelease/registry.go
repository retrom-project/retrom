package payloadrelease

import (
	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

type Ownership = application.Ownership
type OwnershipEdge = application.OwnershipEdge

func OwnershipRegistry() []OwnershipEdge { return application.OwnershipRegistry() }

func ValidateOwnershipRegistry() error {
	edges, err := repository.LoadLifecycleBlobEdges()
	if err != nil {
		return err
	}
	return application.ValidateOwnershipRegistry(edges)
}
