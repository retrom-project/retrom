package emulationstationimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type ScanPublication struct {
	repository model.ScanRepository
	now        func() time.Time
}

func NewScanPublication(repository model.ScanRepository, now func() time.Time) *ScanPublication {
	return &ScanPublication{repository: repository, now: now}
}

func (service *ScanPublication) Reset(ctx context.Context, unit model.Execution) error {
	return service.withOwner(ctx, unit, func(scope model.ScanScope, change model.ScanMutation) error {
		return scope.Write.Clear(ctx, change)
	})
}

func (service *ScanPublication) Headers(ctx context.Context, unit model.Execution, value model.ScanProjection) error {
	return service.withOwner(ctx, unit, func(scope model.ScanScope, change model.ScanMutation) error {
		return scope.Write.Headers(ctx, change, value)
	})
}

func (service *ScanPublication) Items(ctx context.Context, unit model.Execution, items []model.ScanItem) error {
	for offset := 0; offset < len(items); offset += 500 {
		batch := items[offset:min(offset+500, len(items))]
		if err := service.withOwner(ctx, unit, func(scope model.ScanScope, change model.ScanMutation) error {
			return scope.Write.Items(ctx, change, batch)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (service *ScanPublication) Finish(ctx context.Context, unit model.Execution, value model.ScanProjection) error {
	return service.withOwner(ctx, unit, func(scope model.ScanScope, change model.ScanMutation) error {
		return scope.Write.Complete(ctx, change, value)
	})
}

func (service *ScanPublication) Rejected(ctx context.Context, unit model.Execution, value model.ScanProjection) error {
	return service.withOwner(ctx, unit, func(scope model.ScanScope, change model.ScanMutation) error {
		if err := scope.Write.Headers(ctx, change, value); err != nil {
			return fmt.Errorf("persist rejected scan headers: %w", err)
		}
		return scope.Write.Reject(ctx, change, value)
	})
}

func (service *ScanPublication) Publish(ctx context.Context, unit model.Execution, value model.ScanProjection) error {
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
	unit model.Execution,
	write func(model.ScanScope, model.ScanMutation) error,
) error {
	if unit.Kind != "SERVER_EMULATIONSTATION_SCAN" {
		return model.ErrInvalid
	}
	err := service.repository.WithScan(ctx, func(scope model.ScanScope) error {
		current, found, err := scope.Read.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation scan owner: %w", err)
		}
		if !found {
			return model.ErrVersionConflict
		}
		now := service.now().UnixMilli()
		switch ExecutionState(current, unit, now) {
		case model.LeaseActive:
		case model.LeaseDeadline:
			return model.ErrExpired
		case model.LeaseLost, model.LeaseCancelled:
			return model.ErrVersionConflict
		}
		return write(scope, model.ScanMutation{Before: current, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	return nil
}
