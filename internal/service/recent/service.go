package recent

import (
	"context"
	"fmt"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

type Service struct{ Repository *persistence.Repository }

func (s *Service) List(ctx context.Context, p model.Principal, q model.Query) (model.Page[model.Recent], error) {
	result, err := s.Repository.RecentGames(ctx, p.User.ID, q)
	if err != nil {
		return result, fmt.Errorf("recent games: %w", err)
	}
	return result, nil
}
