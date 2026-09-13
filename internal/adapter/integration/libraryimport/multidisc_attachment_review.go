package libraryimport

import (
	"context"
	"fmt"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func (service *Service) ReviewMultiDisc(
	ctx context.Context,
	itemID string,
) (any, bool, error) {
	records := repository.BindReviewDependencies(service.database)
	head, err := records.Head(ctx, itemID)
	if err != nil {
		return nil, false, fmt.Errorf("read review dependency headline: %w", err)
	}
	result, found, err := application.NewReviewDependencies(records).MultiDisc(ctx, itemID, head)
	if err != nil {
		return nil, false, fmt.Errorf("read content review dependencies: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return legacyMultiDiscProjection(&result), true, nil
}

func (service *Service) ReviewArcadeDependencies(ctx context.Context, itemID string) (any, bool, error) {
	records := repository.BindReviewDependencies(service.database)
	head, err := records.Head(ctx, itemID)
	if err != nil {
		return nil, false, fmt.Errorf("read review dependency headline: %w", err)
	}
	result, found, err := application.NewReviewDependencies(records).Arcade(ctx, itemID, head)
	if err != nil {
		return nil, false, fmt.Errorf("read content review dependencies: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return legacyArcadeProjection(&result), true, nil
}
