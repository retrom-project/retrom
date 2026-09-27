package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"

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
		return gamerelease.Owner(ctx, records.executor, scope)
	case application.ScopeImportItem, application.ScopeImportJob:
		return itemrelease.Owner(ctx, records.executor, scope)
	case application.ScopeSourceImportItem:
		return sourcerelease.Owner(ctx, records.executor, scope)
	default:
		return application.Owner{}, application.ErrScopeInvalid
	}
}

func (records scheduling) PendingChildren(ctx context.Context, id string) (int64, error) {
	return itemrelease.PendingChildren(ctx, records.executor, id)
}

func (records scheduling) Consumption(ctx context.Context, id string) (application.Consumption, error) {
	var result application.Consumption
	var released sql.NullInt64
	err := dbapi.QueryRowContext(
		ctx, records.executor, `SELECT version,released_at_ms FROM upload_consumptions WHERE id=?`, id).
		Scan(&result.Version, &released)
	if err != nil {
		return application.Consumption{}, fmt.Errorf("read release consumption: %w", err)
	}
	result.Released = released.Valid
	if result.Released {
		return result, nil
	}
	err = dbapi.QueryRowContext(ctx, records.executor, `SELECT id FROM jobs
WHERE kind='PAYLOAD_RELEASE' AND scope_type='UPLOAD_CONSUMPTION' AND scope_id=?`, id).Scan(&result.ExistingJobID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return application.Consumption{}, fmt.Errorf("read scheduled consumption job: %w", err)
	}
	return result, nil
}
