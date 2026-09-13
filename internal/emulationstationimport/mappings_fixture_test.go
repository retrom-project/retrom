package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) UpdateMappings(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	mappings []Mapping,
) (Summary, error) {
	var actorID string
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	result, err := application.NewMappings(repository.NewMappings(service.database), service.tags, service.now).
		Update(ctx, importID, expectedVersion, mappings, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("update EmulationStation mappings: %w", err)
	}
	return result, nil
}
