package launch

import (
	"context"
	"fmt"

	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

func (service *Service) Config(ctx context.Context, id, capability string) (Config, error) {
	configuration, err := service.configIssuer().Issue(ctx, application.SessionRef{ID: id}, capability)
	if err != nil {
		return Config{}, fmt.Errorf("launch config: %w", err)
	}
	return configuration, nil
}

func (service *Service) configIssuer() *application.ConfigIssuer {
	return application.NewConfigIssuer(persistence.NewConfig(service.database), service.runtimeBuilder,
		application.ConfigEnvironment{
			Now: service.now, Matches: retromruntime.MatchesCapability, PublicOrigin: service.publicOrigin,
			SignIsolation: func(id string) (application.IsolationTicket, error) {
				origin, ticket, hash, err := service.isolatedRuntimeTicket(id)
				return application.IsolationTicket{Origin: origin, Ticket: ticket, Hash: hash}, err
			},
		})
}

func (service *Service) MultiDiscTelemetryDimensions(
	ctx context.Context,
	launchID, capability string,
) (MultiDiscTelemetryDimensions, error) {
	result, err := service.sessionQueries().MultiDiscTelemetryDimensions(ctx, launchID, capability)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}

func (service *Service) BundleFiles(ctx context.Context, launchID, capability, kind string) ([]BundleFile, error) {
	result, err := service.sessionQueries().BundleFiles(ctx, application.SessionRef{ID: launchID}, capability, kind)
	if err != nil {
		return result, fmt.Errorf("launch resource query: %w", err)
	}
	return result, nil
}
