package sourceimport

import (
	"context"
	"fmt"
)

func (service *Materialization) SetPhase(ctx context.Context, id ExecutionIdentity, phase string) error {
	if phase != "COPYING_CONTENT" && phase != "VALIDATING" {
		return ErrInvalid
	}
	err := service.repository.WithMaterialization(ctx, func(scope MaterialScope) error {
		before, err := scope.Read.Execution(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read execution phase: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before.Execution, id, now); err != nil {
			return err
		}
		if before.Execution.Kind != "IMPORT_RECEIVE" || before.Execution.JobState != "RUNNING" {
			return ErrVersionConflict
		}
		if before.Phase == phase {
			return nil
		}
		return scope.Write.Phase(ctx, PhaseChange{Before: before, Phase: phase, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("set Source execution phase: %w", err)
	}
	return nil
}

func (service *Materialization) Cancelled(ctx context.Context, id ExecutionIdentity) (bool, error) {
	cancelled := false
	err := service.repository.WithMaterialization(ctx, func(scope MaterialScope) error {
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
		return false, fmt.Errorf("read Source cancellation checkpoint: %w", err)
	}
	return cancelled, nil
}
