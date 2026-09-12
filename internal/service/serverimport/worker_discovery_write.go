package serverimport

import (
	"context"
	"fmt"
)

func (service *Service) clearEvaluation(ctx context.Context, unit work) error {
	if err := service.discovery.Reset(ctx, unit); err != nil {
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
	if err := service.discovery.Persist(ctx, unit, groups, counts); err != nil {
		return fmt.Errorf("persist import discovery: %w", err)
	}
	return nil
}
