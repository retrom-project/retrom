package launch

import (
	"context"
	"encoding/json"
	"fmt"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
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

func MarshalConfig(config Config) ([]byte, error) {
	contents, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal launch config: %w", err)
	}
	return contents, nil
}
