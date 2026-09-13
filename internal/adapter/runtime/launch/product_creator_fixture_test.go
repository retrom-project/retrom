package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

func (service *Service) Create(ctx context.Context, profileID string, request CreateRequest) (Created, error) {
	result, err := service.productCreator(persistence.NewProductCreation(service.database)).Create(
		ctx,
		application.ProductCreateCommand{ProfileID: profileID, Request: request},
	)
	if err != nil {
		return Created{}, fmt.Errorf("launch product creation: %w", err)
	}
	return result.Created, nil
}

func (service *Service) CreateProduct(
	ctx context.Context,
	command application.ProductCreateCommand,
) (application.ProductReceipt, error) {
	result, err := service.productCreator(persistence.NewProductCreation(service.database)).Create(ctx, command)
	if err != nil {
		return application.ProductReceipt{}, fmt.Errorf("launch product receipt: %w", err)
	}
	return result, nil
}

func (service *Service) productCreator(repository application.ProductCreationRepository) *application.ProductCreator {
	var provider application.PreviewProvider
	if service.runtimeBuilder != nil {
		provider = service.runtimeBuilder
	}
	return application.NewProductCreator(
		repository,
		provider,
		productBlobVerifier{blobs: service.blobs},
		application.ProductEnvironment{
			Now: service.now, SignCapability: service.signPreviewCapability,
			ResumeValidation: service.validationRuns.Dispatch,
			SignIsolation: func(id string) (application.IsolationTicket, error) {
				origin, ticket, hash, err := service.isolatedRuntimeTicket(id)
				if err != nil {
					return application.IsolationTicket{}, err
				}
				return application.IsolationTicket{Origin: origin, Ticket: ticket, Hash: hash}, nil
			},
		},
	)
}
