package pegasusimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/pegasusimport"
)

func (service *Materialization) SetPhase(ctx context.Context, id model.ExecutionIdentity, phase string) error {
	if phase != "COPYING_CONTENT" && phase != "VALIDATING" {
		return model.ErrInvalid
	}
	err := service.repository.WithMaterialization(ctx, func(scope model.MaterialScope) error {
		before, err := scope.Read.Execution(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read execution phase: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before.Execution, id, now); err != nil {
			return err
		}
		if before.Execution.Kind != "SERVER_PEGASUS_IMPORT" || before.Execution.JobState != "RUNNING" {
			return model.ErrVersionConflict
		}
		if before.Phase == phase {
			return nil
		}
		return scope.Write.Phase(ctx, model.PhaseChange{Before: before, Phase: phase, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("set Pegasus execution phase: %w", err)
	}
	return nil
}

func (service *Materialization) Cancelled(ctx context.Context, id model.ExecutionIdentity) (bool, error) {
	cancelled := false
	err := service.repository.WithMaterialization(ctx, func(scope model.MaterialScope) error {
		before, err := scope.Read.Execution(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read execution phase: %w", err)
		}
		if err := ValidateExecution(before.Execution, id, service.now().UnixMilli()); err != nil {
			return err
		}
		cancelled = before.Execution.JobState == "CANCEL_REQUESTED"
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("read Pegasus cancellation checkpoint: %w", err)
	}
	return cancelled, nil
}
