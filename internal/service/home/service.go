package home

import (
	"context"
	"fmt"
	"sort"
)

type Service struct {
	repository Repository
	tags       TagReader
}

func New(repository Repository, tags TagReader) *Service {
	return &Service{repository: repository, tags: tags}
}

func (service *Service) Dashboard(ctx context.Context, profileID string) (Data, error) {
	summary, err := service.repository.Summary(ctx, profileID)
	if err != nil {
		return Data{}, fmt.Errorf("read home summary: %w", err)
	}
	recentPage, err := service.RecentPage(ctx, RecentQuery{ProfileID: profileID, Sort: RecentSortRecent, Limit: 11})
	if err != nil {
		return Data{}, err
	}
	recentSaves, err := service.RecentSaves(ctx, profileID)
	if err != nil {
		return Data{}, err
	}
	latestGames, err := service.LatestGames(ctx)
	if err != nil {
		return Data{}, err
	}
	featured, found, err := service.FeaturedGame(ctx, profileID)
	if err != nil {
		return Data{}, err
	}
	platforms, err := service.repository.Platforms(ctx, profileID)
	if err != nil {
		return Data{}, fmt.Errorf("read home platforms: %w", err)
	}
	quick := append([]Platform(nil), platforms...)
	sort.Slice(quick, func(left, right int) bool {
		if quick[left].PlayCount != quick[right].PlayCount {
			return quick[left].PlayCount > quick[right].PlayCount
		}
		if quick[left].Name != quick[right].Name {
			return quick[left].Name < quick[right].Name
		}
		return quick[left].ID < quick[right].ID
	})
	if len(quick) > 4 {
		quick = quick[:4]
	}
	if !found {
		featured = FeaturedGame{}
	}
	result := Data{
		Summary: summary, LatestGames: latestGames, RecentGames: recentPage.Items,
		RecentSaves: recentSaves, Platforms: platforms, QuickPlatforms: quick,
	}
	if found {
		result.FeaturedGame = &featured
	}
	result.RecentGames = homeRecentPreview(recentPage.Items, result.FeaturedGame)
	return result, nil
}

func (service *Service) RecentSaves(ctx context.Context, profileID string) ([]RecentSave, error) {
	saves, err := service.repository.RecentSaves(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("read recent saves: %w", err)
	}
	ids := make([]string, 0, len(saves))
	for _, item := range saves {
		ids = append(ids, item.GameID)
	}
	if err := service.attachTags(ctx, ids, func(index int, tags []Tag) {
		saves[index].Tags = tags
	}); err != nil {
		return nil, err
	}
	return saves, nil
}

func (service *Service) LatestGames(ctx context.Context) ([]LatestGame, error) {
	games, err := service.repository.LatestGames(ctx)
	if err != nil {
		return nil, fmt.Errorf("read latest games: %w", err)
	}
	ids := make([]string, 0, len(games))
	for _, item := range games {
		ids = append(ids, item.GameID)
	}
	if err := service.attachTags(ctx, ids, func(index int, tags []Tag) {
		games[index].Tags = tags
	}); err != nil {
		return nil, err
	}
	return games, nil
}

func (service *Service) FeaturedGame(ctx context.Context, profileID string) (FeaturedGame, bool, error) {
	game, found, err := service.repository.FeaturedGame(ctx, profileID)
	if err != nil {
		return FeaturedGame{}, false, fmt.Errorf("read featured game: %w", err)
	}
	if !found {
		return FeaturedGame{}, false, nil
	}
	if err := service.attachTags(ctx, []string{game.GameID}, func(_ int, tags []Tag) {
		game.Tags = tags
	}); err != nil {
		return FeaturedGame{}, false, err
	}
	return game, true, nil
}

func (service *Service) attachTags(ctx context.Context, ids []string, assign func(int, []Tag)) error {
	if service.tags == nil {
		return nil
	}
	references, err := service.tags.References(ctx, ids)
	if err != nil {
		return fmt.Errorf("project home tags: %w", err)
	}
	for index, id := range ids {
		tags := references[id]
		if tags == nil {
			tags = []Tag{}
		}
		assign(index, tags)
	}
	return nil
}

func recentGameIDs(games []RecentGame) []string {
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.GameID)
	}
	return ids
}

func homeRecentPreview(games []RecentGame, featured *FeaturedGame) []RecentGame {
	preview := make([]RecentGame, 0, 10)
	for _, game := range games {
		if featured != nil && game.GameID == featured.GameID {
			continue
		}
		preview = append(preview, game)
		if len(preview) == 10 {
			break
		}
	}
	return preview
}
