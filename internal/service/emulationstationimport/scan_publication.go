package emulationstationimport

import (
	"context"
	"fmt"
	"time"
)

type ScanPublication struct {
	repository ScanRepository
	now        func() time.Time
}

func NewScanPublication(repository ScanRepository, now func() time.Time) *ScanPublication {
	return &ScanPublication{repository: repository, now: now}
}

func (service *ScanPublication) Reset(ctx context.Context, unit Execution) error {
	return service.withOwner(ctx, unit, func(scope ScanScope, change ScanMutation) error {
		return scope.Write.Clear(ctx, change)
	})
}

func (service *ScanPublication) Headers(ctx context.Context, unit Execution, value ScanProjection) error {
	return service.withOwner(ctx, unit, func(scope ScanScope, change ScanMutation) error {
		return scope.Write.Headers(ctx, change, value)
	})
}

func (service *ScanPublication) Items(ctx context.Context, unit Execution, items []ScanItem) error {
	for offset := 0; offset < len(items); offset += 500 {
		batch := items[offset:min(offset+500, len(items))]
		if err := service.withOwner(ctx, unit, func(scope ScanScope, change ScanMutation) error {
			return scope.Write.Items(ctx, change, batch)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (service *ScanPublication) Finish(ctx context.Context, unit Execution, value ScanProjection) error {
	return service.withOwner(ctx, unit, func(scope ScanScope, change ScanMutation) error {
		return scope.Write.Complete(ctx, change, value)
	})
}

func (service *ScanPublication) Rejected(ctx context.Context, unit Execution, value ScanProjection) error {
	return service.withOwner(ctx, unit, func(scope ScanScope, change ScanMutation) error {
		if err := scope.Write.Headers(ctx, change, value); err != nil {
			return fmt.Errorf("persist rejected scan headers: %w", err)
		}
		return scope.Write.Reject(ctx, change, value)
	})
}

func (service *ScanPublication) Publish(ctx context.Context, unit Execution, value ScanProjection) error {
	if err := service.Headers(ctx, unit, value); err != nil {
		return err
	}
	if err := service.Items(ctx, unit, value.Items); err != nil {
		return err
	}
	return service.Finish(ctx, unit, value)
}

func (service *ScanPublication) withOwner(
	ctx context.Context,
	unit Execution,
	write func(ScanScope, ScanMutation) error,
) error {
	if unit.Kind != "SERVER_EMULATIONSTATION_SCAN" {
		return ErrInvalid
	}
	err := service.repository.WithScan(ctx, func(scope ScanScope) error {
		current, found, err := scope.Read.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation scan owner: %w", err)
		}
		if !found {
			return ErrVersionConflict
		}
		now := service.now().UnixMilli()
		switch ExecutionState(current, unit, now) {
		case LeaseActive:
		case LeaseDeadline:
			return ErrExpired
		case LeaseLost, LeaseCancelled:
			return ErrVersionConflict
		}
		return write(scope, ScanMutation{Before: current, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	return nil
}
