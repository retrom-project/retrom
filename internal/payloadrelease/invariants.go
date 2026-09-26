package payloadrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

var ErrLifecycleInvariant = application.ErrLifecycleInvariant

func validateLifecycleState(ctx context.Context, database dbapi.DB) error {
	if err := application.NewLifecycleVerifier(repository.NewLifecycle(database)).Validate(ctx); err != nil {
		return fmt.Errorf("validate payload lifecycle state: %w", err)
	}
	return nil
}
