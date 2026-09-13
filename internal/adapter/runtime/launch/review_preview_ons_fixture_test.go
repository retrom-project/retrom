package launch

import (
	"context"
	"fmt"

	retromruntime "retrom/internal/adapter/runtime/runtime"
	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

func (service *Service) projectIndexes() *application.ProjectIndexes {
	return application.NewProjectIndexes(
		persistence.NewProjectIndexes(service.database),
		service.now,
		retromruntime.MatchesCapability,
	)
}

func (service *Service) ProjectIndex(ctx context.Context, id, capability string) (ProjectIndexView, error) {
	result, err := service.projectIndexes().Index(ctx, application.ProjectIndexReference{ID: id}, capability)
	if err != nil {
		return result, fmt.Errorf("launch project index: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewProjectIndex(
	ctx context.Context,
	id, capability string,
) (ProjectIndexView, error) {
	result, err := service.projectIndexes().Index(
		ctx,
		application.ProjectIndexReference{ID: id, PreviewOnly: true},
		capability,
	)
	if err != nil {
		return result, fmt.Errorf("review project index: %w", err)
	}
	return result, nil
}

func (service *Service) ReviewPreviewProjectContent(
	ctx context.Context,
	previewID, capability, logicalName string,
) (ContentView, error) {
	result, err := service.contentAccess().PreviewProjectContent(ctx, previewID, capability, logicalName)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}
