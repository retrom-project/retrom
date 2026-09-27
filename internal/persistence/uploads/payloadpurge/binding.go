package payloadpurge

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseschedule"
	application "retrom/internal/service/cleanupjobs"
)

type scheduling struct{ releaseschedule.Records }

func BindScheduling(executor dbapi.Executor) application.ConsumptionSchedulingScope {
	return scheduling{releaseschedule.Records{Executor: executor}}
}

func (records scheduling) Consumption(ctx context.Context, id string) (application.Consumption, error) {
	return SchedulingConsumption(ctx, records.Executor, id)
}
