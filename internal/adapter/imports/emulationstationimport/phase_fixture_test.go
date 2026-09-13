package emulationstationimport

import (
	"context"
	"fmt"
)

func (service *Service) updateExecutionPhase(ctx context.Context, unit work, phase string) error {
	if err := service.materialization().SetPhase(ctx, unit, phase); err != nil {
		return fmt.Errorf("update EmulationStation phase: %w", err)
	}
	return nil
}
