package sourceimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/sourceimport"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/sourceimport"
)

func (service *Service) workerSettlement() *application.WorkerSettlement {
	return application.NewWorkerSettlement(
		repository.NewWorkerSettlement(service.database),
		library.NewMetadataSeeder(nil, service.now),
		service.now,
	)
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	closed, err := service.workerSettlement().Cancelled(ctx, unit.Identity())
	if err != nil {
		return false, fmt.Errorf("sourceimport/close cancellation: %w", err)
	}
	return closed, nil
}
