package serverimport

import (
	"context"
	"fmt"

	servermodel "retrom/internal/model/serverimport"
)

func (service *Service) clearEvaluation(ctx context.Context, unit servermodel.Work) error {
	if err := service.discovery.Reset(ctx, unit); err != nil {
		return fmt.Errorf("reset import discovery: %w", err)
	}
	return nil
}

func (service *Service) persistCandidates(
	ctx context.Context,
	unit servermodel.Work,
	groups map[string][]*EvaluatedCandidate,
	counts servermodel.DiscoveryCounts,
) error {
	if err := service.discovery.Persist(ctx, unit, groups, counts); err != nil {
		return fmt.Errorf("persist import discovery: %w", err)
	}
	return nil
}
