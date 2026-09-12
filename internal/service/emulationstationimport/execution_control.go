package emulationstationimport

import (
	"context"
	"fmt"
	"time"

	library "retrom/internal/service/libraryimport"
)

type (
	ExecutionFailure struct {
		Code      string
		Retryable bool
	}
	ExecutionFinish struct {
		Before                                        LeaseSnapshot
		NowMS, AvailableAtMS                          int64
		JobState, ImportState, ItemState, Phase, Code string
		Retryable                                     bool
	}
	ExecutionReader interface {
		Current(context.Context, string) (LeaseSnapshot, bool, error)
		TerminalCount(context.Context, string) (int64, error)
		Reviews(context.Context, string, int) ([]ExecutionReview, error)
	}
	ExecutionWriter interface {
		Finish(context.Context, ExecutionFinish) error
		Fence(context.Context, LeaseSnapshot, int64) error
		CompleteReview(context.Context, ExecutionReviewCompletion) error
	}
	ExecutionScope struct {
		Read     ExecutionReader
		Write    ExecutionWriter
		Metadata library.MetadataScope
	}
	ExecutionRepository interface {
		WithExecution(context.Context, func(ExecutionScope) error) error
	}
	ExecutionControl struct {
		repository ExecutionRepository
		now        func() time.Time
	}
)

func NewExecutionControl(repository ExecutionRepository, now func() time.Time) *ExecutionControl {
	return &ExecutionControl{repository: repository, now: now}
}

func (service *ExecutionControl) Observe(ctx context.Context, unit Execution) (LeaseState, error) {
	state := LeaseLost
	err := service.repository.WithExecution(ctx, func(scope ExecutionScope) error {
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
		return LeaseLost, fmt.Errorf("observe EmulationStation execution: %w", err)
	}
	return state, nil
}

func (service *ExecutionControl) CloseCancelled(ctx context.Context, unit Execution) (bool, error) {
	closed := false
	for !closed {
		more := false
		err := service.repository.WithExecution(ctx, func(scope ExecutionScope) error {
			before, err := currentExecution(ctx, scope.Read, unit)
			if err != nil {
				return err
			}
			now := service.now().UnixMilli()
			state := ExecutionState(before, unit, now)
			if state == LeaseActive {
				return nil
			}
			if state != LeaseCancelled || before.LeaseUntilMS <= now || before.DeadlineAtMS <= now {
				return ErrVersionConflict
			}
			before, more, err = service.completeReviews(ctx, scope, before)
			if err != nil {
				return err
			}
			if more {
				return nil
			}
			change := ExecutionFinish{
				Before:      before,
				NowMS:       service.now().UnixMilli(),
				JobState:    "CANCELLED",
				ImportState: "CANCELLED",
				ItemState:   "CANCELLED",
			}
			if err := scope.Write.Finish(ctx, change); err != nil {
				return fmt.Errorf("persist EmulationStation cancellation acknowledgement: %w", err)
			}
			closed = true
			return nil
		})
		if err != nil {
			return false, fmt.Errorf("acknowledge EmulationStation cancellation: %w", err)
		}
		if !more {
			return closed, nil
		}
	}
	return closed, nil
}

func currentExecution(ctx context.Context, reader ExecutionReader, unit Execution) (LeaseSnapshot, error) {
	before, found, err := reader.Current(ctx, unit.JobID)
	if err != nil {
		return LeaseSnapshot{}, fmt.Errorf("read current EmulationStation execution: %w", err)
	}
	if !found || before.Execution != unit {
		return LeaseSnapshot{}, ErrVersionConflict
	}
	return before, nil
}
