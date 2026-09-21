package launch

import (
	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimelaunch"
	application "retrom/internal/service/launch"
)

func (service *Service) contentAccess() *application.ContentAccess {
	return application.NewContentAccess(
		persistence.NewContentQueries(service.database),
		service.now,
		retromruntime.MatchesCapability,
	)
}

func (service *Service) sessionQueries() *application.SessionQueries {
	return application.NewSessionQueries(
		persistence.NewSessionQueries(service.database),
		runtimeTargetAssets{service.runtimeBuilder},

		service.now,
		retromruntime.MatchesCapability,
	)
}

type runtimeTargetAssets struct{ builder *runtimelaunch.Builder }

func (assets runtimeTargetAssets) AssetPaths(providerID, targetID string) ([]string, bool) {
	if assets.builder == nil {
		return nil, false
	}
	target, found := assets.builder.Target(providerID, targetID)
	return target.AssetPaths, found
}
