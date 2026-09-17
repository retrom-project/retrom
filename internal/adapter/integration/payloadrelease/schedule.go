package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	persistence "retrom/internal/repo/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
)

type ScopeType = payloadreleasemodel.ScopeType

const (
	ScopeImportItem                 = payloadreleasemodel.ScopeImportItem
	ScopeImportJob                  = payloadreleasemodel.ScopeImportJob
	ScopePegasusImportItem          = payloadreleasemodel.ScopePegasusImportItem
	ScopeEmulationStationImportItem = payloadreleasemodel.ScopeEmulationStationImportItem
	ScopeUploadConsumption          = payloadreleasemodel.ScopeUploadConsumption
	ScopeGame                       = payloadreleasemodel.ScopeGame
	ScopeBlob                       = payloadreleasemodel.ScopeBlob
)

type Reason = payloadreleasemodel.Reason

const (
	ReasonImportPublished          = payloadreleasemodel.ReasonImportPublished
	ReasonImportDiscarded          = payloadreleasemodel.ReasonImportDiscarded
	ReasonImportFailed             = payloadreleasemodel.ReasonImportFailed
	ReasonImportCancelled          = payloadreleasemodel.ReasonImportCancelled
	ReasonImportTerminal           = payloadreleasemodel.ReasonImportTerminal
	ReasonPegasusTerminal          = payloadreleasemodel.ReasonPegasusTerminal
	ReasonEmulationStationTerminal = payloadreleasemodel.ReasonEmulationStationTerminal
	ReasonUploadConsumed           = payloadreleasemodel.ReasonUploadConsumed
	ReasonGameDeleted              = payloadreleasemodel.ReasonGameDeleted
)

var ErrScopeInvalid = payloadreleasemodel.ErrScopeInvalid

type scheduleInput = payloadreleasemodel.Input

func Schedule(ctx context.Context, transaction *sql.Tx, scopeType ScopeType, scopeID string,
	scopeVersion int64, reason Reason, now int64,
) (string, error) {
	scope := persistence.BindScheduling(transaction)
	scheduler := payloadreleaseservice.NewScheduler(nil)
	return scheduledIdentity(scheduler.Queue(ctx, scope, payloadreleasemodel.ScheduleRequest{
		Scope: payloadreleasemodel.Scope{
			Type: scopeType,
			ID:   scopeID,
		}, ScopeVersion: scopeVersion, Reason: reason, NowMS: now,
	}))
}

func scheduledIdentity(id string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("schedule payload release: %w", err)
	}
	return id, nil
}
