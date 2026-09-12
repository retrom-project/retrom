package metadatascrape

import (
	"context"
	"fmt"

	workpersistence "retrom/internal/persistence/metadatascrape"
	workservice "retrom/internal/service/metadatascrape"
)

func (service *Service) Run(ctx context.Context, runID string) error {
	repository := workpersistence.NewWorker(service.database)
	lookup := workservice.NewLookup(
		workpersistence.NewCache(
			service.database,
		),
		service.blobs,
		service.provider,
		service.now,
	)
	recorder := workservice.NewRecorder(workpersistence.NewRecorder(service.database), service.blobs, service.now)
	assets := workservice.NewAssets(
		workpersistence.NewAssets(
			service.database,
		),
		service.provider,
		service.blobs,
		service.now,
	)
	processor := workservice.NewProcessor(repository, lookup, recorder, assets)
	if err := workservice.NewWorker(repository, processor, service.now).Run(ctx, runID); err != nil {
		return fmt.Errorf("run metadata worker: %w", err)
	}
	return nil
}
