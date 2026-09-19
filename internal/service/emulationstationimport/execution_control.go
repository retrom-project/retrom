package emulationstationimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type ExecutionControl struct {
	repository model.ExecutionRepository
	now        func() time.Time
}

func NewExecutionControl(repository model.ExecutionRepository, now func() time.Time) *ExecutionControl {
	return &ExecutionControl{repository: repository, now: now}
}

func (service *ExecutionControl) Observe(ctx context.Context, unit model.Execution) (model.LeaseState, error) {
	before, found, err := service.repository.CurrentExecution(ctx, unit.JobID)
	if err != nil {
		return model.LeaseLost, fmt.Errorf("observe EmulationStation execution: %w", err)
	}
	if !found {
		return model.LeaseLost, nil
	}
	return ExecutionState(before, unit, service.now().UnixMilli()), nil
}

func (service *ExecutionControl) CloseCancelled(ctx context.Context, unit model.Execution) (bool, error) {
	for {
		closed, more, err := service.closeCancelledAttempt(ctx, unit)
		if err != nil {
			return false, fmt.Errorf("acknowledge EmulationStation cancellation: %w", err)
		}
		if closed || !more {
			return closed, nil
		}
	}
}

func (service *ExecutionControl) closeCancelledAttempt(
	ctx context.Context, unit model.Execution,
) (bool, bool, error) {
	before, found, err := service.repository.CurrentExecution(ctx, unit.JobID)
	if err != nil {
		return false, false, fmt.Errorf("read EmulationStation cancellation: %w", err)
	}
	if !found || before.Execution != unit {
		return false, false, model.ErrVersionConflict
	}
	now := service.now().UnixMilli()
	state := ExecutionState(before, unit, now)
	if state == model.LeaseActive {
		return false, false, nil
	}
	if state != model.LeaseCancelled || before.LeaseUntilMS <= now || before.DeadlineAtMS <= now {
		return false, false, model.ErrVersionConflict
	}
	result, err := service.repository.CommitExecutionReviewBatch(
		ctx, unit, func() int64 { return service.now().UnixMilli() }, before.ReleaseYearMax,
	)
	if err != nil {
		return false, false, err
	}
	if result.More {
		return false, true, nil
	}
	change := model.ExecutionFinish{
		Before:      result.Before,
		NowMS:       service.now().UnixMilli(),
		JobState:    "CANCELLED",
		ImportState: "CANCELLED",
		ItemState:   "CANCELLED",
	}
	planExecutionProjection(&change)
	if err := service.repository.CommitExecutionFinish(ctx, change); err != nil {
		return false, false, fmt.Errorf("persist EmulationStation cancellation: %w", err)
	}
	return true, false, nil
}

func currentExecution(
	ctx context.Context,
	reader model.ExecutionSnapshotReader,
	unit model.Execution,
) (model.LeaseSnapshot, error) {
	before, found, err := reader.Current(ctx, unit.JobID)
	if err != nil {
		return model.LeaseSnapshot{}, fmt.Errorf("read current EmulationStation execution: %w", err)
	}
	if !found || before.Execution != unit {
		return model.LeaseSnapshot{}, model.ErrVersionConflict
	}
	return before, nil
}
