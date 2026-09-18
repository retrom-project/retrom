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
	change, err := service.ownerChange(ctx, unit)
	if err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	if err := service.repository.CommitScanClear(ctx, change); err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	return nil
}

func (service *ScanPublication) Headers(
	ctx context.Context, unit model.Execution, value model.ScanProjection,
) error {
	change, err := service.ownerChange(ctx, unit)
	if err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	if err := service.repository.CommitScanHeaders(ctx, change, value); err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	return nil
}

func (service *ScanPublication) Items(
	ctx context.Context, unit model.Execution, items []model.ScanItem,
) error {
	for offset := 0; offset < len(items); offset += 500 {
		batch := items[offset:min(offset+500, len(items))]
		change, err := service.ownerChange(ctx, unit)
		if err != nil {
			return fmt.Errorf("publish EmulationStation scan projection: %w", err)
		}
		if err := service.repository.CommitScanItems(ctx, change, batch); err != nil {
			return fmt.Errorf("publish EmulationStation scan projection: %w", err)
		}
	}
	return nil
}

func (service *ScanPublication) Finish(
	ctx context.Context, unit model.Execution, value model.ScanProjection,
) error {
	change, err := service.ownerChange(ctx, unit)
	if err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	if err := service.repository.CommitScanComplete(ctx, change, value); err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	return nil
}

func (service *ScanPublication) Rejected(
	ctx context.Context, unit model.Execution, value model.ScanProjection,
) error {
	change, err := service.ownerChange(ctx, unit)
	if err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	if err := service.repository.CommitScanRejection(ctx, change, value); err != nil {
		return fmt.Errorf("publish EmulationStation scan projection: %w", err)
	}
	return nil
}

func (service *ScanPublication) Publish(
	ctx context.Context, unit model.Execution, value model.ScanProjection,
) error {
	if err := service.Headers(ctx, unit, value); err != nil {
		return err
	}
	if err := service.Items(ctx, unit, value.Items); err != nil {
		return err
	}
	return service.Finish(ctx, unit, value)
}

func (service *ScanPublication) ownerChange(
	ctx context.Context, unit model.Execution,
) (model.ScanMutation, error) {
	if unit.Kind != "SERVER_EMULATIONSTATION_SCAN" {
		return model.ScanMutation{}, model.ErrInvalid
	}
	current, found, err := service.repository.LoadScanOwner(ctx, unit.JobID)
	if err != nil {
		return model.ScanMutation{}, fmt.Errorf(
			"read EmulationStation scan owner: %w", err,
		)
	}
	if !found {
		return model.ScanMutation{}, model.ErrVersionConflict
	}
	now := service.now().UnixMilli()
	switch ExecutionState(current, unit, now) {
	case model.LeaseActive:
	case model.LeaseDeadline:
		return model.ScanMutation{}, model.ErrExpired
	case model.LeaseLost, model.LeaseCancelled:
		return model.ScanMutation{}, model.ErrVersionConflict
	}
	return model.ScanMutation{Before: current, NowMS: now}, nil
}
