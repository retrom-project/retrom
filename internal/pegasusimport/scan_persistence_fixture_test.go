package pegasusimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) scanPublication() *application.ScanPublication {
	return application.NewScanPublication(repository.NewScanPublication(service.database), service.now)
}

func (service *Service) persistScan(ctx context.Context, unit work, result scanResult) error {
	if err := service.scanPublication().Save(ctx, unit.Identity(), result.Projection()); err != nil {
		return fmt.Errorf("publish Pegasus scan: %w", err)
	}
	return nil
}

func (service *Service) persistScanHeaders(ctx context.Context, unit work, result scanResult, _ int64) error {
	if err := service.scanPublication().Headers(ctx, unit.Identity(), result.Projection().Headers); err != nil {
		return fmt.Errorf("stage Pegasus scan headers: %w", err)
	}
	return nil
}

func (service *Service) persistScanItems(ctx context.Context, unit work, items []scannedItem, _ int64) error {
	return service.scanPublication().Items(ctx, unit.Identity(), items)
}

func (service *Service) finishScan(ctx context.Context, unit work, result scanResult, _ int64) error {
	if err := service.scanPublication().Finish(ctx, unit.Identity(), result.Projection().Summary); err != nil {
		return fmt.Errorf("finish Pegasus scan: %w", err)
	}
	return nil
}
