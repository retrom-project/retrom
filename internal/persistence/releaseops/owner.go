package releaseops

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

func ReadOwner(ctx context.Context, executor dbapi.Executor, scope application.Scope,
	query string,
) (application.Owner, error) {
	owner := application.Owner{Scope: scope}
	err := dbapi.QueryRowContext(ctx, executor, query, scope.ID).Scan(&owner.State, &owner.Version,
		&owner.PayloadState, &owner.ReleaseJobID, &owner.PublicID, &owner.Retryable)
	if err != nil {
		return application.Owner{}, fmt.Errorf("read payload owner: %w", err)
	}
	return owner, nil
}

func ScheduleWrite(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write payload owner: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count payload owner writes: %w", err)
	}
	if rows != 1 {
		return application.ErrScopeInvalid
	}
	return nil
}
