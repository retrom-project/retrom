package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/persistence/launch"
)

func (service *Service) ensureVariant(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	requestedCore string,
	launchWhenReady bool,
) (Created, error) {
	if launchWhenReady {
		return service.Create(ctx, profileID, request)
	}
	result, err := service.productCreator(persistence.NewProductCreation(service.database)).EnsureVariant(
		ctx,
		request.GameID,
		requestedCore,
		request.ClientCapabilities,
	)
	if err != nil {
		return Created{}, fmt.Errorf("launch ensure variant: %w", err)
	}
	return result, nil
}

func (service *Service) ResumeValidationJob(ctx context.Context, jobID string) {
	service.resumeValidationJob(ctx, jobID)
}

func (service *Service) EnsureVariantForMove(ctx context.Context, gameID, coreID string) (Created, error) {
	selected := coreID
	return service.ensureVariant(ctx, "", CreateRequest{
		GameID: gameID, CoreID: &selected, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
	}, coreID, false)
}
