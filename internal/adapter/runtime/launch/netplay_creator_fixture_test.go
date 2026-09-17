package launch

import (
	"context"
	"fmt"

	launchmodel "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
	launchservice "retrom/internal/service/launch"
)

func (service *Service) CreateNetplay(ctx context.Context, request NetplayCreateRequest) (Created, error) {
	result, err := service.netplayCreator(persistence.NewNetplayCreation(service.database)).CreateNetplay(ctx, request)
	if err != nil {
		return Created{}, fmt.Errorf("launch netplay creation: %w", err)
	}
	return result, nil
}

func (service *Service) netplayCreator(repository launchmodel.NetplayCreationRepository) *launchservice.NetplayCreator {
	var provider launchmodel.PreviewProvider
	if service.runtimeBuilder != nil {
		provider = service.runtimeBuilder
	}
	return launchservice.NewNetplayCreator(
		repository,
		provider,
		productBlobVerifier{blobs: service.blobs},
		launchmodel.NetplayCreationEnvironment{
			Now: service.now, SignCapability: service.signPreviewCapability,
		},
	)
}
