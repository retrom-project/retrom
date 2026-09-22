package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	persistence "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

type ScopeType = application.ScopeType

const (
	ScopeImportItem        = application.ScopeImportItem
	ScopeImportJob         = application.ScopeImportJob
	ScopeSourceImportItem  = application.ScopeSourceImportItem
	ScopeUploadConsumption = application.ScopeUploadConsumption
	ScopeGame              = application.ScopeGame
	ScopeBlob              = application.ScopeBlob
)

type Reason = application.Reason

const (
	ReasonImportPublished = application.ReasonImportPublished
	ReasonImportDiscarded = application.ReasonImportDiscarded
	ReasonImportFailed    = application.ReasonImportFailed
	ReasonImportCancelled = application.ReasonImportCancelled
	ReasonImportTerminal  = application.ReasonImportTerminal
	ReasonSourceTerminal  = application.ReasonSourceTerminal
	ReasonGameDeleted     = application.ReasonGameDeleted
)

var ErrScopeInvalid = application.ErrScopeInvalid

type scheduleInput = application.Input

func Schedule(ctx context.Context, transaction *sql.Tx, scopeType ScopeType, scopeID string,
	scopeVersion int64, reason Reason, now int64,
) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := application.NewScheduler(nil)
	return scheduledIdentity(scheduler.Queue(ctx, scope, application.ScheduleRequest{
		Scope: application.Scope{Type: scopeType, ID: scopeID}, ScopeVersion: scopeVersion, Reason: reason, NowMS: now,
	}))
}

func scheduledIdentity(id string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("schedule payload release: %w", err)
	}
	return id, nil
}
