package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/capability/security/authn"
	persistence "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) Delete(ctx context.Context, id string, version int64) error {
	actorID := ""
	if principal, ok := authn.PrincipalFromContext(ctx); ok {
		actorID = principal.UserID
	}
	lifecycle := application.NewPlanLifecycle(persistence.NewPlanLifecycle(service.database), service.now)
	if err := lifecycle.Delete(ctx, id, version, actorID); err != nil {
		return fmt.Errorf("delete EmulationStation plan: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	lifecycle := application.NewPlanLifecycle(persistence.NewPlanLifecycle(service.database), service.now)
	if err := lifecycle.Expire(ctx); err != nil {
		return fmt.Errorf("expire EmulationStation plans: %w", err)
	}
	return nil
}
