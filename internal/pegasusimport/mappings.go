package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) UpdateMappings(
	ctx context.Context,
	id string,
	version int64,
	mappings []Mapping,
) (Summary, error) {
	actorID := ""
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	mapper := application.NewMappings(repository.NewMappings(service.database), service.tags, service.now)
	result, err := mapper.Update(ctx, id, version, mappings, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("update Pegasus mappings: %w", err)
	}
	return result, nil
}
