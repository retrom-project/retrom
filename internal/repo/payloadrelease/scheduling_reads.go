package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
)

type scheduling struct{ executor dbexec.Executor }

// BindScheduling participates in the caller's existing owner-transition transaction.
func BindScheduling(executor dbexec.Executor) application.SchedulingScope {
	return scheduling{executor}
}

func (records scheduling) Owner(ctx context.Context, scope application.Scope) (application.Owner, error) {
	query, err := ownerQuery(scope.Type)
	if err != nil {
		return application.Owner{}, err
	}
	owner := application.Owner{Scope: scope}
	err = records.executor.QueryRowContext(ctx, query, scope.ID).Scan(&owner.State, &owner.Version,
		&owner.PayloadState, &owner.ReleaseJobID, &owner.PublicID, &owner.Retryable)
	if err != nil {
		return application.Owner{}, fmt.Errorf("read payload scheduling owner: %w", err)
	}
	return owner, nil
}

func ownerQuery(scope application.ScopeType) (string, error) {
	switch scope {
	case application.ScopeImportItem:
		return `SELECT state,version,payload_state,COALESCE(payload_release_job_id,''),'',0
FROM import_items WHERE id=?`, nil
	case application.ScopeImportJob:
		return `SELECT state,version,payload_state,COALESCE(payload_release_job_id,''),'',0
FROM import_jobs WHERE id=?`, nil
	case application.ScopeGame:
		return `SELECT status,version,payload_state,COALESCE(payload_release_job_id,''),'',0
FROM games WHERE id=?`, nil
	case application.ScopePegasusImportItem:
		return `SELECT execution_state,version,payload_state,COALESCE(payload_release_job_id,''),
COALESCE(library_import_item_id,''),retryable FROM pegasus_import_items WHERE id=?`, nil
	case application.ScopeEmulationStationImportItem:
		return `SELECT execution_state,version,payload_state,COALESCE(payload_release_job_id,''),
COALESCE(library_import_item_id,''),retryable FROM emulationstation_import_items WHERE id=?`, nil
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return "", application.ErrScopeInvalid
	default:
		return "", application.ErrScopeInvalid
	}
}

func (records scheduling) PendingChildren(ctx context.Context, id string) (int64, error) {
	var count int64
	err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM import_items WHERE import_job_id=?
AND state NOT IN ('PUBLISHED','DISCARDED','FAILED_FINAL','CANCELLED')`, id).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("read pending payload owners: %w", err)
	}
	return count, nil
}

func (records scheduling) Consumption(ctx context.Context, id string) (application.Consumption, error) {
	var result application.Consumption
	var released sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `SELECT version,released_at_ms FROM upload_consumptions WHERE id=?`, id).
		Scan(&result.Version, &released)
	if err != nil {
		return application.Consumption{}, fmt.Errorf("read release consumption: %w", err)
	}
	result.Released = released.Valid
	if result.Released {
		return result, nil
	}
	err = records.executor.QueryRowContext(ctx, `SELECT id FROM jobs
WHERE kind='PAYLOAD_RELEASE' AND scope_type='UPLOAD_CONSUMPTION' AND scope_id=?`, id).Scan(&result.ExistingJobID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return application.Consumption{}, fmt.Errorf("read scheduled consumption job: %w", err)
	}
	return result, nil
}
