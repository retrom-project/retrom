package payloadrelease

import (
	"context"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	uploads "retrom/internal/persistence/uploads/payloadpurge"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

type scheduling struct{ executor dbapi.Executor }

// BindScheduling participates in the caller's existing owner-transition transaction.
func BindScheduling(executor dbapi.Executor) application.SchedulingScope {
	return scheduling{executor}
}

func (records scheduling) Owner(ctx context.Context, scope application.Scope) (application.Owner, error) {
	switch scope.Type {
	case application.ScopeGame:
		return wrapPair(gamerelease.Owner(ctx, records.executor, scope))
	case application.ScopeImportItem, application.ScopeImportJob:
		return wrapPair(itemrelease.Owner(ctx, records.executor, scope))
	case application.ScopeSourceImportItem:
		return wrapPair(sourcerelease.Owner(ctx, records.executor, scope))
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return application.Owner{}, application.ErrScopeInvalid
	default:
		return application.Owner{}, application.ErrScopeInvalid
	}
}

func (records scheduling) PendingChildren(ctx context.Context, id string) (int64, error) {
	return wrapPair(itemrelease.PendingChildren(ctx, records.executor, id))
}

func (records scheduling) Consumption(ctx context.Context, id string) (application.Consumption, error) {
	return wrapPair(uploads.SchedulingConsumption(ctx, records.executor, id))
}
