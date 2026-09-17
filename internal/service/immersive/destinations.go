package immersive

import (
	"context"

	model "retrom/internal/model/immersive"
)

func (service *Service) Destinations(ctx context.Context, profileID string) ([]model.Destination, error) {
	result, err := service.repository.LoadDestinations(ctx, profileID)
	return result, repositoryError("destinations", err)
}
