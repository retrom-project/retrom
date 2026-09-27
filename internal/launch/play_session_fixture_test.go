package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

func (service *Service) RecordPlaySnapshot(ctx context.Context, id, capability string, sample PlaySnapshot) (application.PlaySnapshotResult, error) {
	controller := application.NewPlayController(persistence.NewPlay(service.database), service.now, retromruntime.MatchesCapability)
	result, err := controller.RecordSnapshot(ctx, id, capability, sample)
	if err != nil {
		return application.PlaySnapshotResult{}, fmt.Errorf("play snapshot: %w", err)
	}
	return result, nil
}

func (service *Service) FinishReviewPreview(ctx context.Context, id, capability string) error {
	closer := application.NewPreviewCloser(persistence.NewPreviewClose(service.database), service.now, retromruntime.MatchesCapability)
	return closer.Finish(ctx, id, capability)
}
