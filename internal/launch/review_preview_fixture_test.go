package launch

import (
	"context"
	"fmt"
	"io"

	libraryimport "retrom/internal/libraryimport"
	reviewrepository "retrom/internal/persistence/libraryimport"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
	reviewservice "retrom/internal/service/libraryimport"
)

func (service *Service) ReviewPreviewConfig(ctx context.Context, id, capability string) (Config, error) {
	configuration, err := service.configIssuer().Issue(ctx, application.SessionRef{ID: id, Preview: true}, capability)
	if err != nil {
		return Config{}, fmt.Errorf("review config: %w", err)
	}
	return configuration, nil
}

func (service *Service) StoreReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	reader io.Reader,
) (reviewservice.ReviewScreenshot, error) {
	result, err := service.screenshotSaver(reviewrepository.NewScreenshots(service.database)).Store(
		ctx, previewID, capability, reader,
	)
	if err != nil {
		return reviewservice.ReviewScreenshot{}, fmt.Errorf("store review screenshot: %w", err)
	}
	return result, nil
}

func (service *Service) screenshotSaver(repository reviewservice.ScreenshotRepository) *reviewservice.ScreenshotSaver {
	var images reviewservice.ScreenshotImages
	if service.blobs != nil {
		images = libraryimport.NewReviewScreenshotImages(service.blobs)
	}
	return reviewservice.NewScreenshotSaver(repository, images, reviewservice.ScreenshotEnvironment{
		Now: service.now, Matches: retromruntime.MatchesCapability,
	})
}
