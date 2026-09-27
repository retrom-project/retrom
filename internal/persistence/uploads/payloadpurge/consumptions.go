package payloadpurge

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

func ReadConsumptions(
	ctx context.Context, executor dbapi.Executor,
	query string,
	args ...any,
) ([]application.EffectConsumption, error) {
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query release consumptions: %w", err)
	}
	defer func() { cleanup.Error("close release consumptions", rows.Close()) }()
	values := make([]application.EffectConsumption, 0)
	for rows.Next() {
		var value application.EffectConsumption
		var released sql.NullInt64
		if err := rows.Scan(
			&value.ID,
			&value.SessionID,
			&value.FileID,
			&value.ConsumerType,
			&value.ConsumerID,
			&value.Version,
			&released,
		); err != nil {
			return nil, fmt.Errorf("decode release consumption: %w", err)
		}
		value.Released = application.WorkTime{Set: released.Valid, Value: released.Int64}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate release consumptions: %w", err)
	}
	return values, nil
}

func JobConsumptions(ctx context.Context, executor dbapi.Executor, id string) ([]application.EffectConsumption, error) {
	return ReadConsumptions(ctx, executor,
		`SELECT id,upload_session_id,COALESCE(upload_file_id,''),consumer_type,consumer_id,version,released_at_ms
 FROM upload_consumptions WHERE consumer_type='IMPORT_JOB' AND consumer_id=?`, id)
}
