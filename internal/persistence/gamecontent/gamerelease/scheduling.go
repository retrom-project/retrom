package gamerelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseschedule"
	application "retrom/internal/service/payloadrelease"
)

type scheduling struct{ releaseschedule.Records }

func BindScheduling(executor dbapi.Executor) application.OwnerSchedulingScope {
	return scheduling{releaseschedule.Records{Executor: executor}}
}

func (records scheduling) Owner(ctx context.Context, scope application.Scope) (application.Owner, error) {
	if scope.Type != application.ScopeGame {
		return application.Owner{}, application.ErrScopeInvalid
	}
	return Owner(ctx, records.Executor, scope)
}

func (records scheduling) BeginRelease(ctx context.Context, change application.OwnerRelease) error {
	scope := change.Before.Scope
	if scope.Type != application.ScopeGame {
		return application.ErrScopeInvalid
	}
	return Begin(ctx, records.Executor, releaseschedule.OwnerUpdate(change.Before, change.JobID), change)
}
