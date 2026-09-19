package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
)

func (service *Materialization) SetPhase(ctx context.Context, unit model.Execution, phase string) error {
	if phase != "COPYING_CONTENT" && phase != "VALIDATING" && phase != "PREPARING_REVIEWS" {
		return model.ErrInvalid
	}
	err := service.repository.WithMaterialization(ctx, func(scope model.MaterialScope) error {
		before, err := scope.Read.Execution(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation phase: %w", err)
		}
		now := service.now().UnixMilli()
		if err := validateImportExecution(before.Execution, unit, now); err != nil {
			return err
		}
		if before.Phase == phase {
			return nil
		}
		return scope.Write.Phase(ctx, model.PhaseChange{Before: before, Phase: phase, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("set EmulationStation phase: %w", err)
	}
	return nil
}
