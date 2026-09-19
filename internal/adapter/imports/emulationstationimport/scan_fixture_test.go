package emulationstationimport

import (
	"context"
	"errors"
	"fmt"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) persistScanHeaders(ctx context.Context, unit work, value scanResult) error {
	if err := service.scanPublication().Headers(ctx, unit, emulationstationimportmodel.ScanProjection(value)); err != nil {
		return fmt.Errorf("persist EmulationStation scan headers: %w", err)
	}
	return nil
}

func (service *Service) persistScanItems(ctx context.Context, unit work, items []scannedItem) error {
	if err := service.scanPublication().Items(ctx, unit, items); err != nil {
		return fmt.Errorf("persist EmulationStation scan items: %w", err)
	}
	return nil
}

func (service *Service) finishScan(ctx context.Context, unit work, value scanResult) error {
	if err := service.scanPublication().Finish(ctx, unit, emulationstationimportmodel.ScanProjection(value)); err != nil {
		return fmt.Errorf("finish EmulationStation scan: %w", err)
	}
	return nil
}

func (service *Service) executeScan(ctx context.Context, unit work, _ Root) {
	if err := service.scanExecutor().Execute(ctx, unit); err != nil {
		service.executionDispatcher().Failed(ctx, unit, err)
	}
}

func (service *Service) fail(ctx context.Context, unit work, retryable bool) {
	service.executionDispatcher().Failed(
		ctx,
		unit,
		&application.ExecutionError{Failure: emulationstationimportmodel.ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: retryable}, Cause: errors.New("INTERNAL_ERROR")},
	)
}
