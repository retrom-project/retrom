package immersive

import (
	"context"
	"fmt"

	model "retrom/internal/model/immersive"
)

type Service struct{ repository model.Repository }

func New(repository model.Repository) *Service { return &Service{repository: repository} }
func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("immersive: %s: %w", operation, err)
}

func (service *Service) Platforms(ctx context.Context, profileID string) ([]model.Platform, error) {
	var result []model.Platform
	err := service.repository.WithRead(ctx, func(scope model.ReadScope) error {
		var err error
		result, err = readPlatforms(ctx, scope.Platforms, profileID)
		return err
	})
	return result, repositoryError("platforms", err)
}

func readPlatforms(ctx context.Context, reader model.PlatformReader, profileID string) ([]model.Platform, error) {
	platforms, err := reader.Platforms(ctx, profileID)
	if err != nil {
		return nil, repositoryError("read platforms", err)
	}
	games, err := reader.Featured(ctx, profileID, "")
	if err != nil {
		return nil, repositoryError("read featured games", err)
	}
	attachFeaturedGames(platforms, games)
	return platforms, nil
}

func (service *Service) Games(
	ctx context.Context,
	profileID, platformID string,
	limit int,
	cursor *model.GameCursor,
) (model.GamePage, error) {
	var result model.GamePage
	err := service.repository.WithRead(ctx, func(scope model.ReadScope) error {
		var err error
		result, err = readGamePage(ctx, scope.Platforms, profileID, platformID, limit, cursor)
		return err
	})
	return result, repositoryError("games", err)
}

func readGamePage(
	ctx context.Context,
	reader model.PlatformReader,
	profileID, platformID string,
	limit int,
	cursor *model.GameCursor,
) (model.GamePage, error) {
	platform, err := reader.Platform(ctx, profileID, platformID)
	if err != nil {
		return model.GamePage{}, repositoryError("read platform", err)
	}
	platform.FeaturedGames, err = reader.Featured(ctx, profileID, platformID)
	if err != nil {
		return model.GamePage{}, repositoryError("read featured games", err)
	}
	games, err := reader.Games(ctx, profileID, platformID, limit, cursor)
	if err != nil {
		return model.GamePage{}, repositoryError("read games", err)
	}
	games, next := libraryPageItems(games, limit, model.LibraryAll)
	return model.GamePage{Platform: platform, Items: games, NextCursor: next}, nil
}

func attachFeaturedGames(platforms []model.Platform, games []model.FeaturedGame) {
	platformIndexes := make(map[string]int, len(platforms))
	for index := range platforms {
		platforms[index].FeaturedGames = make([]model.FeaturedGame, 0, 3)
		platformIndexes[platforms[index].ID] = index
	}
	for _, game := range games {
		index, found := platformIndexes[game.PlatformID]
		if found {
			platforms[index].FeaturedGames = append(platforms[index].FeaturedGames, game)
		}
	}
}
