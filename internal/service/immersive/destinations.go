package immersive

import (
	"context"

	model "retrom/internal/model/immersive"
)

func libraryName(kind string) string {
	switch kind {
	case model.LibraryAll:
		return "全部游戏"
	case model.LibraryRecent:
		return "最近游玩"
	case model.LibraryFavorites:
		return "收藏游戏"
	case model.LibrarySaves:
		return "我的存档"
	default:
		return ""
	}
}

func (service *Service) Destinations(ctx context.Context, profileID string) ([]model.Destination, error) {
	var result []model.Destination
	err := service.repository.WithRead(ctx, func(scope model.ReadScope) error {
		var err error
		result, err = readDestinations(ctx, scope, profileID)
		return err
	})
	return result, repositoryError("destinations", err)
}

func readDestinations(ctx context.Context, scope model.ReadScope, profileID string) ([]model.Destination, error) {
	destinations := make([]model.Destination, 0, 4)
	for _, kind := range []string{model.LibraryAll, model.LibraryRecent, model.LibraryFavorites, model.LibrarySaves} {
		destination, queryErr := scope.Libraries.Summary(ctx, profileID, kind, "")
		if queryErr != nil {
			return nil, repositoryError("read destination", queryErr)
		}
		destination.FeaturedGames, queryErr = scope.Libraries.Featured(
			ctx,
			profileID,
			kind,
			"",
		)
		if queryErr != nil {
			return nil, repositoryError("read destination", queryErr)
		}
		destination.Name = libraryName(kind)
		destinations = append(destinations, destination)
	}
	platforms, err := scope.Platforms.Platforms(ctx, profileID)
	if err != nil {
		return nil, repositoryError("read platforms", err)
	}
	featuredGames, err := scope.Platforms.Featured(ctx, profileID, "")
	if err != nil {
		return nil, repositoryError("read platforms", err)
	}
	attachFeaturedGames(platforms, featuredGames)
	for _, platform := range platforms {
		destinations = append(destinations, model.Destination{
			ID:             platform.ID,
			Kind:           "platform",
			Name:           platform.Name,
			GameCount:      platform.GameCount,
			LastPlayedAtMS: platform.LastPlayedAtMS,
			FeaturedGames:  platform.FeaturedGames,
		})
	}
	return destinations, nil
}
