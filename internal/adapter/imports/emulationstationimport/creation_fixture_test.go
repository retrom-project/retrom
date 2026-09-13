package emulationstationimport

import (
	"context"
	"fmt"

	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) Create(ctx context.Context, request CreateRequest, userID string) (Summary, error) {
	creator := application.NewCreation(
		persistence.NewCreation(service.database),
		service.sources(),
		service.now,
	)
	value, err := creator.Create(ctx, request, userID)
	if err != nil {
		return Summary{}, fmt.Errorf("create EmulationStation scan plan: %w", err)
	}
	service.signal()
	return value, nil
}
