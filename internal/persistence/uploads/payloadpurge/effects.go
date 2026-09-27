package payloadpurge

import (
	"context"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseeffects"
	application "retrom/internal/service/payloadrelease"
)

func BindUploads(executor dbapi.Executor) releaseeffects.Uploads {
	return releaseeffects.Uploads{Owner: func(
		ctx context.Context,
		scope application.Scope,
	) (application.EffectOwner, error) {
		return ConsumptionOwner(ctx, executor, scope)
	}, Files: Records{
		Executor: executor,
	}, Consume: func(
		ctx context.Context,
		change application.EffectConsumptionChange,
	) error {
		return Consume(ctx, executor, change)
	}}
}

func NewEffects(database dbapi.DB) *releaseeffects.Repository {
	return releaseeffects.New(database, func(executor dbapi.Executor) application.EffectScope {
		domain := releaseeffects.Domain{Owner: func(
			ctx context.Context,
			scope application.Scope,
		) (application.EffectOwner, error) {
			return ConsumptionOwner(ctx, executor, scope)
		}}
		return releaseeffects.Bind(executor, domain, BindUploads(executor))
	})
}
