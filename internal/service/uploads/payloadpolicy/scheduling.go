package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
)

func Consumption(ctx context.Context, service *jobs.Scheduler,
	scope jobs.ConsumptionSchedulingScope, id string, now int64,
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
	jobID, err := service.Queue(ctx, scope, jobs.ScheduleRequest{
		Scope:        jobs.Scope{Type: jobs.ScopeUploadConsumption, ID: id},
		ScopeVersion: before.Version, Reason: jobs.ReasonUploadConsumed, NowMS: now,
	})
	if err != nil {
		return "", fmt.Errorf("schedule upload cleanup: %w", err)
	}
	return jobID, nil
}
