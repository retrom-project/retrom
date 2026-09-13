package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/capability/security/authn"
	repository "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) StartImport(ctx context.Context, id string, version int64) (Summary, error) {
	var actorID string
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	sources := service.sources()
	result, queued, err := application.NewStarter(repository.NewStarter(service.database), sources, service.now).
		Start(ctx, id, version, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("start EmulationStation import: %w", err)
	}
	if queued {
		service.signal()
	}
	return result, nil
}
