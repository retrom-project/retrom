package home

import (
	"context"
	"fmt"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/service/saves"
)

type Service struct {
	Repository *persistence.Repository
	Saves      *saves.Service
}

func (s *Service) Get(ctx context.Context, p model.Principal) (model.Home, error) {
	result := model.Home{}
	recent, err := s.Repository.RecentGames(ctx, p.User.ID, model.Query{Limit: 12})
	if err != nil {
		return result, wrap(err)
	}
	result.Recent = recent.Items
	savesPage, err := s.Saves.List(ctx, p, "", model.Query{Limit: 12})
	if err != nil {
		return result, wrap(err)
	}
	result.Saves = savesPage.Items
	result.Summary.SaveCount = savesPage.Total
	favorites, err := s.Repository.Games(ctx, p.User.ID, "published", model.Query{Limit: 12, Favorite: true})
	if err != nil {
		return result, wrap(err)
	}
	result.Favorites = favorites.Items
	result.Directories, err = s.Repository.Directories(ctx, false)
	if err != nil {
		return result, wrap(err)
	}
	result.Summary.GameCount, err = s.Repository.PublishedGameCount(ctx)
	return result, wrap(err)
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("home operation: %w", err)
}
