package serverimport

import (
	"context"
	"fmt"
)

func rankCandidates(values []*evaluatedCandidate) []*evaluatedCandidate {
	return RankCandidates(values)
}

func selectedStatus(candidate *evaluatedCandidate) (string, string) {
	return SelectedStatus(candidate)
}

func (service *Service) loadItems(ctx context.Context, id string) ([]catalogItem, error) {
	result, err := service.recovery.Items(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read frozen items: %w", err)
	}
	return result, nil
}

func (service *Service) discoveryWasPersisted(ctx context.Context, id string) (bool, error) {
	result, err := service.recovery.DiscoveryWasPersisted(ctx, id)
	if err != nil {
		return false, fmt.Errorf("read discovery phase: %w", err)
	}
	return result, nil
}

func (service *Service) loadPersistedCandidates(
	ctx context.Context,
	id string,
	items []catalogItem,
) (map[string][]*evaluatedCandidate, error) {
	result, err := service.recovery.Candidates(ctx, id, items)
	if err != nil {
		return nil, fmt.Errorf("restore candidates: %w", err)
	}
	return result, nil
}
