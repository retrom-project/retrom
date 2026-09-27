package cleanupjobs

import (
	"context"
	"fmt"

	gamepolicy "retrom/internal/service/gamecontent/payloadpolicy"
	sourcepolicy "retrom/internal/service/sourceimport/payloadpolicy"
)

func (service *Scheduler) TerminalSource(
	ctx context.Context, scope OwnerSchedulingScope, ref Scope, now int64,
) (string, error) {
	if ref.Type != ScopeSourceImportItem {
		return "", ErrScopeInvalid
	}
	owner, err := readSchedulingOwner(ctx, scope, ref)
	if err != nil {
		return "", err
	}
	if !sourcepolicy.ReleaseReady(owner.State, owner.Retryable, owner.PublicID) {
		return "", nil
	}
	if owner.PayloadState != "RETAINED" {
		return existingOwnerRelease(owner)
	}
	reason := ReasonSourceTerminal
	return service.scheduleOwner(ctx, scope, owner, reason, now)
}

func (service *Scheduler) Consumption(
	ctx context.Context, scope ConsumptionSchedulingScope, id string, now int64,
) (string, error) {
	before, err := scope.Consumption(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read upload consumption release: %w", err)
	}
	if before.Released {
		return "", nil
	}
	if before.ExistingJobID != "" {
		return before.ExistingJobID, nil
	}
	return service.Queue(ctx, scope, ScheduleRequest{
		Scope:        Scope{Type: ScopeUploadConsumption, ID: id},
		ScopeVersion: before.Version, Reason: ReasonUploadConsumed, NowMS: now,
	})
}

func (service *Scheduler) DeleteGame(
	ctx context.Context, scope OwnerSchedulingScope, id string, version, now int64,
) (string, error) {
	before, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeGame, ID: id})
	if err != nil {
		return "", err
	}
	if !gamepolicy.CanDelete(before.State, before.PayloadState, before.Version, version) {
		return "", ErrScopeInvalid
	}
	jobID, err := service.Queue(ctx, scope, ScheduleRequest{
		Scope: before.Scope, ScopeVersion: version + 1,
		Reason: ReasonGameDeleted, NowMS: now,
	})
	if err != nil {
		return "", err
	}
	change := OwnerRelease{Before: before, JobID: jobID, NowMS: now, DeleteGame: true}
	if err := scope.BeginRelease(ctx, change); err != nil {
		return "", fmt.Errorf("schedule deleted game payload: %w", err)
	}
	return jobID, nil
}
