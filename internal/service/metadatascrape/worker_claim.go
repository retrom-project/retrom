package metadatascrape

import (
	"context"
	"fmt"
	timecontract "time"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

func (worker *Worker) claim(
	ctx context.Context,
	run metadatascrapemodel.WorkerRun,
) (metadatascrapemodel.WorkerClaim, bool, error) {
	workerID, err := scheduleID()
	if err != nil {
		return metadatascrapemodel.WorkerClaim{}, false, err
	}
	claim := metadatascrapemodel.WorkerClaim{
		RunID: run.RunID, JobID: run.JobID, ExecutionNo: run.ExecutionNo,
		WorkerID: workerID, Version: run.Version, AttemptCount: run.AttemptCount,
	}
	claimed := false
	err = worker.repository.WithWrite(ctx, func(scope metadatascrapemodel.WorkerScope) error {
		claim.Now = worker.now().UnixMilli()
		claim.Deadline = run.Deadline
		if claim.Deadline == 0 {
			claim.Deadline = claim.Now + timecontract.Hour.Milliseconds()
		}
		claim.Terminal = claim.Deadline <= claim.Now || run.MaxAttempts > 0 && run.AttemptCount >= run.MaxAttempts ||
			run.JobState == "CANCEL_REQUESTED"
		if run.JobState == "RUNNING" && !claim.Terminal {
			available := min(claim.Now+metadataRetryDelay(run.AttemptCount), claim.Deadline)
			if _, err := scope.Leases.Requeue(ctx, claim, available); err != nil {
				return fmt.Errorf("reschedule expired metadata lease: %w", err)
			}
			return nil
		}
		var err error
		claimed, err = scope.Leases.Claim(ctx, claim)
		if err != nil {
			return fmt.Errorf("claim scrape lease: %w", err)
		}
		return nil
	})
	if err != nil {
		return claim, false, fmt.Errorf("claim metadata execution: %w", err)
	}
	return claim, claimed, nil
}

func (worker *Worker) Recover(ctx context.Context) ([]string, error) {
	ids, err := worker.repository.Recoverable(ctx, worker.now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("recover metadata executions: %w", err)
	}
	return ids, nil
}

func metadataRetryDelay(attempt int64) int64 {
	delays := [...]int64{1000, 5000, 30000, 120000}
	return delays[max(0, min(attempt-1, int64(len(delays)-1)))]
}
