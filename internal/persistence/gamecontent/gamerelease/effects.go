package gamerelease

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseeffects"
	"retrom/internal/persistence/releaseops"
	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/cleanupjobs"
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
		Clear: func(ctx context.Context, before application.EffectOwner, now int64) error {
			return clearPayload(ctx, executor, before, now)
		},
	}
	return releaseeffects.Bind(executor, domain, uploads.BindUploads(executor))
}

func effectRelations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	return Relations(ctx, executor, facts)
}

func effectPayload(
	ctx context.Context,
	executor dbapi.Executor,
	scope application.Scope,
) (application.EffectPayload, error) {
	values, err := Consumptions(ctx, executor, scope.ID)
	return application.EffectPayload{Consumptions: values}, err
}

func effectRemaining(ctx context.Context, executor dbapi.Executor, scope application.Scope) (int64, error) {
	return (Records{Executor: executor}).Remaining(ctx, scope.ID)
}

func clearPayload(ctx context.Context, executor dbapi.Executor, before application.EffectOwner, now int64) error {
	if before.Owner.Scope.Type != application.ScopeGame {
		return application.ErrScopeInvalid
	}
	id := before.Owner.Scope.ID
	records := Records{Executor: executor}
	if err := records.StopRuntime(ctx, id, now); err != nil {
		return wrapErr(err)
	}
	if err := records.ClearEvidence(ctx, id, now); err != nil {
		return wrapErr(err)
	}
	if err := (releaseops.Records{Executor: executor}).RemoveBatches(ctx, DeleteStatements(), id); err != nil {
		return wrapErr(err)
	}
	return nil
}
