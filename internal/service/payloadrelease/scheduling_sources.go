package payloadrelease

import (
	"context"
	"fmt"
	"math"
)

func (service *Scheduler) TerminalSource(
	ctx context.Context, scope SchedulingScope, ref Scope, now int64,
) (string, error) {
	if ref.Type != ScopeSourceImportItem {
		return "", ErrScopeInvalid
	}
	owner, err := readSchedulingOwner(ctx, scope, ref)
	if err != nil {
		return "", err
	}
	if !TerminalSourceItem(owner.State, owner.Retryable) {
		return "", nil
	}
	if owner.PayloadState != "RETAINED" {
		return existingOwnerRelease(owner)
	}
	if owner.PublicID != "" {
		return service.linkSource(ctx, scope, owner, now)
	}
	reason := ReasonSourceTerminal
	return service.scheduleOwner(ctx, scope, owner, reason, now)
}

func (service *Scheduler) linkSource(
	ctx context.Context, scope SchedulingScope, owner Owner, now int64,
) (string, error) {
	if owner.Version == math.MaxInt64 {
		return "", ErrScopeInvalid
	}
	ordinary, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeImportItem, ID: owner.PublicID})
	if err != nil {
		return "", err
	}
	jobID, err := existingOwnerRelease(ordinary)
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, OwnerRelease{Before: owner, JobID: jobID, NowMS: now}); err != nil {
		return "", fmt.Errorf("link source to ordinary payload release: %w", err)
	}
	return jobID, nil
}

func (service *Scheduler) Consumption(
	ctx context.Context, scope SchedulingScope, id string, now int64,
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
	ctx context.Context, scope SchedulingScope, id string, version, now int64,
) (string, error) {
	before, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeGame, ID: id})
	if err != nil {
		return "", err
	}
	if before.Version != version || version == math.MaxInt64 || before.State != "PUBLISHED" ||
		before.PayloadState != "RETAINED" {
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
