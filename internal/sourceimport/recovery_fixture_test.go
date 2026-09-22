package sourceimport

import (
	"context"

	repository "retrom/internal/persistence/sourceimport"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/sourceimport"
)

func (service *Service) recoverWork(ctx context.Context) error {
	return application.NewRecovery(repository.NewRecovery(service.database), library.NewMetadataSeeder(nil, service.now), service.now).Recover(ctx)
}

func (service *Service) maintain(ctx context.Context) error {
	return application.NewMaintenance(application.NewRecovery(repository.NewRecovery(service.database), library.NewMetadataSeeder(nil, service.now), service.now), application.NewPlanLifecycle(repository.NewPlanLifecycle(service.database), service.now)).Maintain(ctx)
}
