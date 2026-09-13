package payloadrelease

import (
	"context"
	"database/sql"
	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

var ErrLifecycleInvariant = application.ErrLifecycleInvariant

func validateLifecycleState(ctx context.Context, database *sql.DB) error {
	return application.NewLifecycleVerifier(repository.NewLifecycle(database)).Validate(ctx)
}
