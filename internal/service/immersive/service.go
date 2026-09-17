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
	result, err := service.repository.LoadPlatforms(ctx, profileID)
	return result, repositoryError("platforms", err)
}

func (service *Service) Games(
	ctx context.Context,
	profileID, platformID string,
	limit int,
	cursor *model.GameCursor,
) (model.GamePage, error) {
	result, err := service.repository.LoadGamePage(ctx, profileID, platformID, limit, cursor)
	return result, repositoryError("games", err)
}
