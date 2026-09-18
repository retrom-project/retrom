package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
)

func (service *Materialization) SetPhase(
	ctx context.Context, unit model.Execution, phase string,
) error {
	if phase != "COPYING_CONTENT" && phase != "VALIDATING" && phase != "PREPARING_REVIEWS" {
		return model.ErrInvalid
	}
	before, err := service.repository.LoadExecutionPhase(ctx, unit.JobID)
	if err != nil {
		return fmt.Errorf("set EmulationStation phase: read EmulationStation phase: %w", err)
	}
	now := service.now().UnixMilli()
	if err := validateImportExecution(before.Execution, unit, now); err != nil {
		return fmt.Errorf("set EmulationStation phase: %w", err)
	}
	if before.Phase == phase {
		return nil
	}
	if err := service.repository.CommitPhaseChange(
		ctx, model.PhaseChange{Before: before, Phase: phase, NowMS: now},
	); err != nil {
		return fmt.Errorf("set EmulationStation phase: %w", err)
	}
	return nil
}
