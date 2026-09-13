package emulationstationimport

import (
	"context"
	"fmt"

	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) checkSourceExecution(ctx context.Context, unit work) error {
	state, err := service.executionControl().Observe(ctx, unit)
	if err != nil {
		return fmt.Errorf("observe EmulationStation source execution: %w: %w", application.ErrExecutionObservation, err)
	}
	switch state {
	case application.LeaseActive:
		return nil
	case application.LeaseCancelled:
		return errImportCancelled
	case application.LeaseLost:
		return application.ErrVersionConflict
	case application.LeaseDeadline:
		return application.ErrExpired
	default:
		return application.ErrVersionConflict
	}
}
