package payloadrelease

import (
	"context"
	"database/sql"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	persistence "retrom/internal/repo/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

func ScheduleTerminalImportItem(ctx context.Context, transaction *sql.Tx, itemID string, reason Reason,
	now int64,
) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalItem(ctx, scope, itemID, reason, now))
}

func ScheduleTerminalImportJob(ctx context.Context, transaction *sql.Tx, importID string, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalImport(ctx, scope, importID, now))
}

func ScheduleTerminalPegasusItem(ctx context.Context, transaction *sql.Tx, itemID string, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalSource(ctx, scope, payloadreleasemodel.Scope{
		Type: ScopePegasusImportItem,
		ID:   itemID,
	}, now))
}

func ScheduleTerminalEmulationStationItem(ctx context.Context, transaction *sql.Tx, itemID string,
	now int64,
) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalSource(ctx, scope, payloadreleasemodel.Scope{
		Type: ScopeEmulationStationImportItem,
		ID:   itemID,
	}, now))
}

func ScheduleConsumption(ctx context.Context, transaction *sql.Tx, consumptionID string, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.Consumption(ctx, scope, consumptionID, now))
}

func ScheduleGameDeletion(ctx context.Context, transaction *sql.Tx, gameID string, version, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.DeleteGame(ctx, scope, gameID, version, now))
}
