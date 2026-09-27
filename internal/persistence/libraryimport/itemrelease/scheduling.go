package itemrelease

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseschedule"
	application "retrom/internal/service/cleanupjobs"
)

type scheduling struct{ releaseschedule.Records }

func BindScheduling(executor dbapi.Executor) application.ItemSchedulingScope {
	return scheduling{releaseschedule.Records{Executor: executor}}
}

func (records scheduling) Owner(ctx context.Context, scope application.Scope) (application.Owner, error) {
	if scope.Type != application.ScopeImportItem && scope.Type != application.ScopeImportJob {
		return application.Owner{}, application.ErrScopeInvalid
	}
	return Owner(ctx, records.Executor, scope)
}

func (records scheduling) BeginRelease(ctx context.Context, change application.OwnerRelease) error {
	scope := change.Before.Scope
	if scope.Type != application.ScopeImportItem && scope.Type != application.ScopeImportJob {
		return application.ErrScopeInvalid
	}
	return Begin(ctx, records.Executor, releaseschedule.OwnerUpdate(change.Before, change.JobID), change)
}

func (records scheduling) PendingChildren(ctx context.Context, id string) (int64, error) {
	return PendingChildren(ctx, records.Executor, id)
}
