package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	repository "retrom/internal/repo/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

var ErrLifecycleInvariant = payloadreleasemodel.ErrLifecycleInvariant

func validateLifecycleState(ctx context.Context, database *sql.DB) error {
	if err := application.NewLifecycleVerifier(repository.NewLifecycle(database)).Validate(ctx); err != nil {
		return fmt.Errorf("validate payload lifecycle state: %w", err)
	}
	return nil
}
