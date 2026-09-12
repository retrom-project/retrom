package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"

	"github.com/google/uuid"
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

func (service *Service) previewCreator(repository application.PreviewCreationRepository) *application.PreviewCreator {
	var provider application.PreviewProvider
	if service.runtimeBuilder != nil {
		provider = service.runtimeBuilder
	}
	return application.NewPreviewCreator(repository, provider, application.PreviewEnvironment{
		Now: service.now, SignCapability: service.signPreviewCapability,
		SignIsolation: func(id string) (application.IsolationTicket, error) {
			origin, ticket, hash, err := service.isolatedRuntimeTicket(id)
			return application.IsolationTicket{Origin: origin, Ticket: ticket, Hash: hash}, err
		},
	})
}

func (service *Service) signPreviewCapability(id string) (string, []byte, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", nil, fmt.Errorf("parse preview identity: %w", err)
	}
	capability := service.credentials.Capability(parsed)
	hash := retromruntime.HashCapability(capability)
	return retromruntime.EncodeCapability(capability), hash[:], nil
}
