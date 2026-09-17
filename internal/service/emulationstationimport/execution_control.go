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
	state := model.LeaseLost
	err := service.repository.WithExecution(ctx, func(scope model.ExecutionScope) error {
		before, found, err := scope.Read.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation execution ownership: %w", err)
		}
		if found {
			state = ExecutionState(before, unit, service.now().UnixMilli())
		}
		return nil
	})
	if err != nil {
		return model.LeaseLost, fmt.Errorf("observe EmulationStation execution: %w", err)
	}
	return state, nil
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
	closed, more := false, false
	err := service.repository.WithExecution(ctx, func(scope model.ExecutionScope) error {
		return service.closeCancelledScope(ctx, scope, unit, &closed, &more)
	})
	if err != nil {
		return false, false, fmt.Errorf("run cancellation acknowledgement scope: %w", err)
	}
	return closed, more, nil
}

func (service *ExecutionControl) closeCancelledScope(
	ctx context.Context,
	scope model.ExecutionScope,
	unit model.Execution,
	closed, more *bool,
) error {
	before, err := currentExecution(ctx, scope.Read, unit)
	if err != nil {
		return err
	}
	now := service.now().UnixMilli()
	state := ExecutionState(before, unit, now)
	if state == model.LeaseActive {
		return nil
	}
	if state != model.LeaseCancelled || before.LeaseUntilMS <= now || before.DeadlineAtMS <= now {
		return model.ErrVersionConflict
	}
	return service.finishCancelledScope(ctx, scope, before, closed, more)
}

func (service *ExecutionControl) finishCancelledScope(
	ctx context.Context,
	scope model.ExecutionScope,
	before model.LeaseSnapshot,
	closed, more *bool,
) error {
	var err error
	before, *more, err = completeExecutionReviews(
		ctx,
		model.ExecutionReviewScope{Read: scope.Read, Write: scope.Write, Metadata: scope.Metadata},
		before,
		service.now,
	)
	if err != nil {
		return err
	}
	if *more {
		return nil
	}
	change := model.ExecutionFinish{
		Before:      before,
		NowMS:       service.now().UnixMilli(),
		JobState:    "CANCELLED",
		ImportState: "CANCELLED",
		ItemState:   "CANCELLED",
	}
	planExecutionProjection(&change)
	if err := scope.Write.Finish(ctx, change); err != nil {
		return fmt.Errorf("persist EmulationStation cancellation acknowledgement: %w", err)
	}
	if change.SchedulePayload {
		if err := scheduleTerminalPayloads(ctx, scope.Payload, change.Before.ImportID, change.NowMS); err != nil {
			return err
		}
	}
	*closed = true
	return nil
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
