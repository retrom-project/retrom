package payloadpurge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

func ConsumptionOwner(ctx context.Context, executor dbapi.Executor,
	scope application.Scope,
) (application.EffectOwner, error) {
	facts := application.EffectOwner{Owner: application.Owner{Scope: scope}}
	facts.Consumption.ID = scope.ID
	var released sql.NullInt64
	err := dbapi.QueryRowContext(ctx, executor,
		`SELECT version,released_at_ms,upload_session_id FROM upload_consumptions WHERE id=?`,
		scope.ID).Scan(&facts.Consumption.Version, &released, &facts.Consumption.SessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return application.EffectOwner{}, fmt.Errorf("read effect consumption: %w", err)
	}
	if err := dbapi.QueryRowContext(ctx, executor,
		`SELECT COALESCE(upload_file_id,''),consumer_type,consumer_id FROM upload_consumptions WHERE id=?`,
		scope.ID).Scan(&facts.Consumption.FileID, &facts.Consumption.ConsumerType,
		&facts.Consumption.ConsumerID); err != nil {
		return application.EffectOwner{}, fmt.Errorf("read effect consumption owner: %w", err)
	}
	facts.Found = true
	facts.Consumption.Released = application.WorkTime{Set: released.Valid, Value: released.Int64}
	facts.Owner.Version = facts.Consumption.Version
	return facts, nil
}
