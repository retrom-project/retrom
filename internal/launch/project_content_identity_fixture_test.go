package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

func (service *Service) ProjectContentIdentity(ctx context.Context, id, capability string) (string, error) {
	queries := application.NewProjectQueries(
		persistence.NewConfig(service.database),
		service.now,
		retromruntime.MatchesCapability,
	)
	identity, err := queries.Identity(ctx, id, capability)
	if err != nil {
		return "", fmt.Errorf("launch project identity: %w", err)
	}
	return identity, nil
}

func (service *Service) ProjectContentRoot(ctx context.Context, id, capability string) (string, error) {
	identity, err := service.ProjectContentIdentity(ctx, id, capability)
	if err != nil {
		return "", err
	}
	return RuntimeProjectContentRoot(identity)
}
