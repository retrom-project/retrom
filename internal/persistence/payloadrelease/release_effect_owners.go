package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	uploads "retrom/internal/persistence/uploads/payloadpurge"

	application "retrom/internal/service/payloadrelease"
)

func (records effectRecords) Owner(ctx context.Context, scope application.Scope) (application.EffectOwner, error) {
	if scope.Type == application.ScopeUploadConsumption {
		return records.consumptionOwner(ctx, scope)
	}
	owner, err := scheduling(records).Owner(ctx, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EffectOwner{Owner: application.Owner{Scope: scope}}, nil
	}
	if err != nil {
		return application.EffectOwner{}, fmt.Errorf("read effect owner: %w", err)
	}
	facts := application.EffectOwner{Owner: owner, Found: true}
	if err := records.ownerRelations(ctx, &facts); err != nil {
		return application.EffectOwner{}, err
	}
	return facts, nil
}

func (records effectRecords) ownerRelations(ctx context.Context, facts *application.EffectOwner) error {
	switch facts.Owner.Scope.Type {
	case application.ScopeGame:
		return wrapErr(gamerelease.Relations(ctx, records.executor, facts))
	case application.ScopeImportItem:
		return wrapErr(itemrelease.Relations(ctx, records.executor, facts))
	case application.ScopeSourceImportItem:
		return wrapErr(sourcerelease.Relations(ctx, records.executor, facts))
	case application.ScopeImportJob:
		return nil
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return application.ErrScopeInvalid
	default:
		return application.ErrScopeInvalid
	}
}

func (records effectRecords) consumptionOwner(ctx context.Context,
	scope application.Scope,
) (application.EffectOwner, error) {
	return wrapPair(uploads.ConsumptionOwner(ctx, records.executor, scope))
}

func (records effectRecords) fenceOwner(ctx context.Context, before application.EffectOwner) error {
	current, err := records.Owner(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("reread effect owner: %w", err)
	}
	if !current.Found || current != before {
		return application.ErrEffectConflict
	}
	return nil
}

func (records effectRecords) Links(ctx context.Context, scope application.Scope) ([]application.Scope, error) {
	switch scope.Type {
	case application.ScopeImportJob:
		return wrapPair(itemrelease.JobLinks(ctx, records.executor, scope.ID))
	case application.ScopeImportItem:
		return wrapPair(sourcerelease.BoundSources(ctx, records.executor, scope.ID))
	case application.ScopeSourceImportItem, application.ScopeUploadConsumption, application.ScopeGame,
		application.ScopeBlob:
		return nil, application.ErrScopeInvalid
	default:
		return nil, application.ErrScopeInvalid
	}
}
