package pegasusimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/pegasusimport"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) recoverWork(ctx context.Context) error {
	err := application.NewRecovery(repository.NewRecovery(service.database),
		library.NewMetadataSeeder(nil, service.now), service.now).Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover Pegasus worker: %w", err)
	}
	return nil
}

func (service *Service) maintain(ctx context.Context) error {
	if err := service.recoverWork(ctx); err != nil {
		return err
	}
	return service.ExpirePlans(ctx)
}
