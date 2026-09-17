package emulationstationimport

import (
	"context"
	"fmt"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func (service *Service) itemWork() *emulationstationimportservice.ItemWork {
	return emulationstationimportservice.NewItemWork(persistence.NewItemWork(service.database), service.now)
}

func (service *Service) materialization() *emulationstationimportservice.Materialization {
	return emulationstationimportservice.NewMaterialization(persistence.NewMaterialization(service.database), service.now)
}

func (service *Service) finishItemOutcome(
	ctx context.Context,
	unit work,
	id string,
	outcome emulationstationimportmodel.ItemOutcome,
) error {
	if err := service.itemWork().Finish(ctx, unit, id, outcome); err != nil {
		return fmt.Errorf("finish EmulationStation item outcome: %w", err)
	}
	return nil
}
