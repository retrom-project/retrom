package metadatascrape

import (
	"context"
	"fmt"

	assetpersistence "retrom/internal/persistence/metadatascrape"
	assetservice "retrom/internal/service/metadatascrape"
)

func (service *Service) fetchPendingAssets(ctx context.Context, runID string) error {
	assets := assetservice.NewAssets(
		assetpersistence.NewAssets(
			service.database,
		),
		service.provider,
		service.blobs,
		service.now,
	)
	if err := assets.Run(ctx, runID); err != nil {
		return fmt.Errorf("fetch scrape assets: %w", err)
	}
	return nil
}
