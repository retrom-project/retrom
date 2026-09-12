package serverimport

import (
	"context"
	"fmt"

	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

func (service *Service) discoveryWriter() *importservice.Discovery {
	return importservice.NewDiscovery(importpersistence.NewDiscovery(service.database), service.now)
}

func (service *Service) clearEvaluation(ctx context.Context, unit work) error {
	if err := service.discoveryWriter().Reset(ctx, unit); err != nil {
		return fmt.Errorf("reset import discovery: %w", err)
	}
	return nil
}

func (service *Service) persistCandidates(
	ctx context.Context,
	unit work,
	groups map[string][]*evaluatedCandidate,
	counts walkCounts,
) error {
	if err := service.discoveryWriter().Persist(ctx, unit, groups, counts); err != nil {
		return fmt.Errorf("persist import discovery: %w", err)
	}
	return nil
}
