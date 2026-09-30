package launch

import (
	"context"
	"fmt"

	variantcomposition "retrom/internal/composition/gamevariant"
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
	variants := variantcomposition.New(service.database, service.sources(), service.now, service.blobs)
	defer variants.Close()
	result, err := variants.Ensure(ctx, request.GameID, requestedCore)
	if err != nil {
		return Created{}, fmt.Errorf("ensure variant: %w", err)
	}
	status := "VALIDATION_PENDING"
	if result.Ready {
		status = "READY"
	}
	return Created{Status: status, JobID: result.JobID, RetryAfterMS: result.RetryAfterMS}, nil
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
