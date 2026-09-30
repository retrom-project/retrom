package launch

import (
	"context"
	"fmt"

	reviewcomposition "retrom/internal/composition/libraryimport"
	persistence "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/launch"
	review "retrom/internal/service/libraryimport"
)

func (service *Service) CreateReviewPreview(
	ctx context.Context,
	request ReviewPreviewRequest,
) (ReviewPreviewCreated, error) {
	result, err := service.previewCreator(persistence.NewReviewPreviewCreation(service.database)).Create(ctx, request)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("review preview creation: %w", err)
	}
	return result, nil
}

func (service *Service) previewCreator(repository review.ReviewPreviewRepository) *review.ReviewPreviews {
	var provider application.PreviewProvider
	if service.runtimeBuilder != nil {
		provider = service.runtimeBuilder
	}
	return review.NewReviewPreviews(repository, provider, reviewcomposition.WithPreviewFiles(application.PreviewEnvironment{
		Now: service.now, SignCapability: service.signPreviewCapability,
		SignIsolation: func(id string) (application.IsolationTicket, error) {
			origin, ticket, hash, err := service.isolatedRuntimeTicket(id)
			return application.IsolationTicket{Origin: origin, Ticket: ticket, Hash: hash}, err
		},
	}, service.blobs))
}

func (service *Service) signPreviewCapability(id string) (string, []byte, error) {
	return service.sources().SignCapability(id)
}
