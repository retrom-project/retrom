package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

func (service *Service) CreateNetplay(ctx context.Context, request NetplayCreateRequest) (Created, error) {
	result, err := service.netplayCreator(persistence.NewNetplayCreation(service.database)).CreateNetplay(ctx, request)
	if err != nil {
		return Created{}, fmt.Errorf("launch netplay creation: %w", err)
	}
	return result, nil
}

func (service *Service) netplayCreator(repository application.NetplayCreationRepository) *application.NetplayCreator {
	var provider application.PreviewProvider
	if service.runtimeBuilder != nil {
		provider = service.runtimeBuilder
	}
	return application.NewNetplayCreator(
		repository,
		provider,
		productBlobVerifier{blobs: service.blobs},
		application.NetplayCreationEnvironment{
			Now: service.now, SignCapability: service.signPreviewCapability,
		},
	)
}
