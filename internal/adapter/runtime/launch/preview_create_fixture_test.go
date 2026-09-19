package launch

import (
	"context"
	"fmt"

	launchmodel "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
	launchservice "retrom/internal/service/launch"
)

func (service *Service) CreateReviewPreview(
	ctx context.Context,
	request ReviewPreviewRequest,
) (ReviewPreviewCreated, error) {
	result, err := service.previewCreator(persistence.NewPreviewCreation(service.database)).Create(ctx, request)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("review preview creation: %w", err)
	}
	return result, nil
}

func (service *Service) previewCreator(repository launchmodel.PreviewCreationRepository) *launchservice.PreviewCreator {
	var provider launchmodel.PreviewProvider
	if service.runtimeBuilder != nil {
		provider = service.runtimeBuilder
	}
	return launchservice.NewPreviewCreator(repository, provider, launchservice.PreviewEnvironment{
		Now: service.now, SignCapability: service.signPreviewCapability,
		SignIsolation: func(id string) (launchmodel.IsolationTicket, error) {
			origin, ticket, hash, err := service.isolatedRuntimeTicket(id)
			return launchmodel.IsolationTicket{Origin: origin, Ticket: ticket, Hash: hash}, err
		},
	})
}

func (service *Service) signPreviewCapability(id string) (string, []byte, error) {
	return service.sources().SignCapability(id)
}
