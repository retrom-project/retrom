package sourcerelease

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
	if scope.Type != application.ScopeSourceImportItem {
		return application.Owner{}, application.ErrScopeInvalid
	}
	return Owner(ctx, records.Executor, scope)
}

func (records scheduling) BeginRelease(ctx context.Context, change application.OwnerRelease) error {
	scope := change.Before.Scope
	if scope.Type != application.ScopeSourceImportItem {
		return application.ErrScopeInvalid
	}
	return Begin(ctx, records.Executor, releaseschedule.OwnerUpdate(change.Before, change.JobID), change)
}

func BindReleases(executor dbapi.Executor) application.ReleaseScope {
	return application.ReleaseScope{
		Scheduling: BindScheduling(executor), Links: scheduling{releaseschedule.Records{Executor: executor}},
	}
}

func (records scheduling) RetainedSources(
	ctx context.Context, batch application.SourceBatch, after string, limit int,
) ([]string, error) {
	return RetainedSources(ctx, records.Executor, batch, after, limit)
}
