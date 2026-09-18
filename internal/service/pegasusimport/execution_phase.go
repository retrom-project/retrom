package pegasusimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/pegasusimport"
)

func (service *Materialization) SetPhase(
	ctx context.Context, id model.ExecutionIdentity, phase string,
) error {
	if phase != "COPYING_CONTENT" && phase != "VALIDATING" {
		return model.ErrInvalid
	}
	before, err := service.repository.LoadExecutionPhase(ctx, id.JobID)
	if err != nil {
		return fmt.Errorf("set Pegasus execution phase: read execution phase: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before.Execution, id, now); err != nil {
		return fmt.Errorf("set Pegasus execution phase: %w", err)
	}
	if before.Execution.Kind != "SERVER_PEGASUS_IMPORT" ||
		before.Execution.JobState != "RUNNING" {
		return fmt.Errorf("set Pegasus execution phase: %w", model.ErrVersionConflict)
	}
	if before.Phase == phase {
		return nil
	}
	if err := service.repository.CommitPhaseChange(
		ctx, model.PhaseChange{Before: before, Phase: phase, NowMS: now},
	); err != nil {
		return fmt.Errorf("set Pegasus execution phase: %w", err)
	}
	return nil
}

func (service *Materialization) Cancelled(
	ctx context.Context, id model.ExecutionIdentity,
) (bool, error) {
	before, err := service.repository.LoadExecutionPhase(ctx, id.JobID)
	if err != nil {
		return false, fmt.Errorf(
			"read Pegasus cancellation checkpoint: read execution phase: %w", err,
		)
	}
	if err := ValidateExecution(
		before.Execution, id, service.now().UnixMilli(),
	); err != nil {
		return false, fmt.Errorf("read Pegasus cancellation checkpoint: %w", err)
	}
	return before.Execution.JobState == "CANCEL_REQUESTED", nil
}
