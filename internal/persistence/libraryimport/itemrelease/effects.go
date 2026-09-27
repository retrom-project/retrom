package itemrelease

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseeffects"
	"retrom/internal/persistence/releaseops"
	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/payloadrelease"
)

func NewEffects(database dbapi.DB) *releaseeffects.Repository {
	return releaseeffects.New(database, BindEffects)
}

func BindEffects(executor dbapi.Executor) application.EffectScope {
	domain := releaseeffects.Domain{
		Owner: func(ctx context.Context, scope application.Scope) (application.EffectOwner, error) {
			return wrapPair(releaseeffects.ReadOwner(ctx, executor, scope, Owner, effectRelations))
		},
		Payload: func(ctx context.Context, scope application.Scope) (application.EffectPayload, error) {
			return effectPayload(ctx, executor, scope)
		},
		Remaining: func(ctx context.Context, scope application.Scope) (int64, error) {
			return effectRemaining(ctx, executor, scope)
		},
		Change: func(
			ctx context.Context,
			update recordstore.Update,
			change application.EffectOwnerChange,
		) (sql.Result, error) {
			return Change(ctx, executor, update, change)
		},
		Remove: func(ctx context.Context, change application.EffectRemoval) error {
			return effectRemove(ctx, executor, change)
		},
	}
	domain.Links = func(ctx context.Context, scope application.Scope) ([]application.Scope, error) {
		if scope.Type != application.ScopeImportJob {
			return nil, application.ErrScopeInvalid
		}
		return JobLinks(ctx, executor, scope.ID)
	}
	return releaseeffects.Bind(executor, domain, uploads.BindUploads(executor))
}

func effectRelations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	if facts.Owner.Scope.Type == application.ScopeImportJob {
		return nil
	}
	return Relations(ctx, executor, facts)
}

func effectPayload(
	ctx context.Context,
	executor dbapi.Executor,
	scope application.Scope,
) (application.EffectPayload, error) {
	if scope.Type == application.ScopeImportJob {
		values, err := uploads.JobConsumptions(ctx, executor, scope.ID)
		return wrapPair(application.EffectPayload{Consumptions: values}, err)
	}
	values, err := Consumptions(ctx, executor, scope.ID)
	return application.EffectPayload{Consumptions: values}, err
}

func effectRemaining(ctx context.Context, executor dbapi.Executor, scope application.Scope) (int64, error) {
	records := Records{Executor: executor}
	if scope.Type == application.ScopeImportJob {
		return records.JobRemaining(ctx, scope.ID)
	}
	return records.ItemRemaining(ctx, scope.ID)
}

func effectRemove(ctx context.Context, executor dbapi.Executor, change application.EffectRemoval) error {
	if change.Before.Owner.Scope.Type != application.ScopeImportItem {
		return application.ErrScopeInvalid
	}
	records := Records{Executor: executor}
	id, now := change.Before.Owner.Scope.ID, change.NowMS
	switch change.Group {
	case application.EffectImportReview:
		return records.ClearReview(ctx, id, now)
	case application.EffectImportEvidence:
		return records.ClearEvidence(ctx, id, now)
	case application.EffectImportFiles:
		return wrapErr((releaseops.Records{Executor: executor}).RemoveBatches(ctx, DeleteStatements(), id))
	case application.EffectGameRuntime,
		application.EffectGameEvidence,
		application.EffectGameFiles,
		application.EffectSourceFiles,
		application.EffectSourceAssets:
		return application.ErrScopeInvalid
	default:
		return application.ErrScopeInvalid
	}
}
