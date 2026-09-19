package launch

import (
	"context"
	"fmt"
	"io"

	retromruntime "retrom/internal/adapter/runtime/runtime"
	launchmodel "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
	launchservice "retrom/internal/service/launch"
)

func (service *Service) ReviewPreviewConfig(ctx context.Context, id, capability string) (Config, error) {
	configuration, err := service.configIssuer().Issue(ctx, launchmodel.SessionRef{ID: id, Preview: true}, capability)
	if err != nil {
		return Config{}, fmt.Errorf("review config: %w", err)
	}
	return configuration, nil
}

func (service *Service) StoreReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	reader io.Reader,
) (ReviewScreenshot, error) {
	result, err := service.screenshotSaver(persistence.NewScreenshots(service.database)).Store(
		ctx, previewID, capability, reader,
	)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("store review screenshot: %w", err)
	}
	return result, nil
}

func (service *Service) screenshotSaver(repository launchmodel.ScreenshotRepository) *launchservice.ScreenshotSaver {
	var images launchmodel.ScreenshotImages
	if service.blobs != nil {
		images = screenshotImages{blobs: service.blobs}
	}
	return launchservice.NewScreenshotSaver(repository, images, launchservice.ScreenshotEnvironment{
		Now: service.now, Matches: retromruntime.MatchesCapability,
	})
}
