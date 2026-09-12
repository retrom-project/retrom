package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	repository "retrom/internal/persistence/pegasusimport"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) workerSettlement() *application.WorkerSettlement {
	return application.NewWorkerSettlement(
		repository.NewWorkerSettlement(service.database),
		library.NewMetadataSeeder(nil, service.now),
		service.now,
	)
}

func (service *Service) fail(ctx context.Context, unit work, code string, retryable bool) {
	if ctx.Err() != nil {
		return
	}
	err := service.workerSettlement().Fail(
		ctx,
		unit.Identity(),
		application.ExecutionFailure{Code: code, Retryable: retryable},
	)
	if err != nil && !errors.Is(err, ErrVersionConflict) {
		slog.Error("Pegasus worker settlement failed", "error", service.sanitizeTechnicalDetail(err))
	}
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	closed, err := service.workerSettlement().Cancelled(ctx, unit.Identity())
	if err != nil {
		return false, fmt.Errorf("pegasusimport/close cancellation: %w", err)
	}
	return closed, nil
}
