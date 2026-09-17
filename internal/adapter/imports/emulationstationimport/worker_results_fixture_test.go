package emulationstationimport

import (
	"context"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
)

func (service *Service) closeItem(ctx context.Context, unit work, itemID, state, code string,
	retryable bool,
) error {
	return service.closeItemWithFailure(ctx, unit, itemID, state, code, retryable, nil)
}

func (service *Service) closeItemWithFailure(ctx context.Context, unit work, itemID, state, code string,
	retryable bool, failure *FailureDetails,
) error {
	return service.finishItemOutcome(ctx, unit, itemID, application.ItemOutcome{
		State: state, Code: code, Retryable: retryable, Failure: failure,
	})
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	closed, err := service.executionControl().CloseCancelled(ctx, unit)
	if err != nil {
		return false, fmt.Errorf("close cancelled EmulationStation execution: %w", err)
	}
	return closed, nil
}
