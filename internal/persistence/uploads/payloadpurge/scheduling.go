package payloadpurge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

func SchedulingConsumption(ctx context.Context, executor dbapi.Executor, id string) (application.Consumption, error) {
	var result application.Consumption
	var released sql.NullInt64
	err := dbapi.QueryRowContext(
		ctx, executor, `SELECT version,released_at_ms FROM upload_consumptions WHERE id=?`, id).
		Scan(&result.Version, &released)
	if err != nil {
		return application.Consumption{}, fmt.Errorf("read release consumption: %w", err)
	}
	result.Released = released.Valid
	if result.Released {
		return result, nil
	}
	err = dbapi.QueryRowContext(ctx, executor, `SELECT id FROM jobs
WHERE kind='OWNER_CLEANUP' AND scope_type='UPLOAD_CONSUMPTION' AND scope_id=?`, id).Scan(&result.ExistingJobID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return application.Consumption{}, fmt.Errorf("read scheduled consumption job: %w", err)
	}
	return result, nil
}
