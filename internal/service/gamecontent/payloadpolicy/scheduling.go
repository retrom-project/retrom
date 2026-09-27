package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
)

func DeleteGame(ctx context.Context, service *jobs.Scheduler, scope jobs.OwnerSchedulingScope,
	id string, version, now int64,
) (string, error) {
	before, err := jobs.ReadSchedulingOwner(ctx, scope, jobs.Scope{Type: jobs.ScopeGame, ID: id})
	if err != nil {
		return "", fmt.Errorf("schedule gamecontent cleanup: %w", err)
	}
	if !CanDelete(before.State, before.PayloadState, before.Version, version) {
		return "", jobs.ErrScopeInvalid
	}
	jobID, err := service.Queue(ctx, scope, jobs.ScheduleRequest{
		Scope: before.Scope, ScopeVersion: version + 1,
		Reason: jobs.ReasonGameDeleted, NowMS: now,
	})
	if err != nil {
		return "", fmt.Errorf("schedule gamecontent cleanup: %w", err)
	}
	change := jobs.OwnerRelease{Before: before, JobID: jobID, NowMS: now, DeleteGame: true}
	if err := scope.BeginRelease(ctx, change); err != nil {
		return "", fmt.Errorf("schedule deleted game payload: %w", err)
	}
	return jobID, nil
}
