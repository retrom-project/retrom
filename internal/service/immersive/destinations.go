package immersive

import (
	"context"
)

func libraryName(kind string) string {
	switch kind {
	case LibraryAll:
		return "全部游戏"
	case LibraryRecent:
		return "最近游玩"
	case LibraryFavorites:
		return "收藏游戏"
	case LibrarySaves:
		return "我的存档"
	default:
		return ""
	}
}

func (service *Service) Destinations(ctx context.Context, profileID string) ([]Destination, error) {
	var result []Destination
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		var err error
		result, err = readDestinations(ctx, scope, profileID)
		return err
	})
	return result, repositoryError("destinations", err)
}

func readDestinations(ctx context.Context, scope ReadScope, profileID string) ([]Destination, error) {
	destinations := make([]Destination, 0, 4)
	for _, kind := range []string{LibraryAll, LibraryRecent, LibraryFavorites, LibrarySaves} {
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
		destinations = append(destinations, Destination{
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
