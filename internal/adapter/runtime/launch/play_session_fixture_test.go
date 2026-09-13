package launch

import (
	"context"
	"fmt"

	retromruntime "retrom/internal/adapter/runtime/runtime"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"
)

func (service *Service) RecordPlay(
	ctx context.Context,
	launchID, capability, kind string,
	event PlayEvent,
) (PlayResult, error) {
	controller := application.NewPlayController(
		persistence.NewPlay(service.database),
		service.now,
		retromruntime.MatchesCapability,
	)
	result, err := controller.RecordPlay(ctx, launchID, capability, kind, event)
	if err != nil {
		return PlayResult{}, fmt.Errorf("launch play: %w", err)
	}
	return result, nil
}
