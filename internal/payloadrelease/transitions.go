package payloadrelease

import (
	"context"

	dbapi "retrom/internal/database"

	persistence "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

func ScheduleTerminalImportItem(ctx context.Context, transaction dbapi.Tx, itemID string, reason Reason,
	now int64,
) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalItem(ctx, scope, itemID, reason, now))
}

func ScheduleTerminalImportJob(ctx context.Context, transaction dbapi.Tx, importID string, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalImport(ctx, scope, importID, now))
}

func ScheduleTerminalSourceItem(ctx context.Context, transaction dbapi.Tx, itemID string, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.TerminalSource(ctx, scope,
		application.Scope{Type: ScopeSourceImportItem, ID: itemID}, now))
}

func ScheduleConsumption(ctx context.Context, transaction dbapi.Tx, consumptionID string, now int64) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.Consumption(ctx, scope, consumptionID, now))
}

func ScheduleGameDeletion(
	ctx context.Context, transaction dbapi.Tx, gameID string, version, now int64,
) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.DeleteGame(ctx, scope, gameID, version, now))
}
