package metadatascrape

import (
	"context"
	"fmt"

	model "retrom/internal/model/metadatascrape"
)

func (worker *Worker) claim(ctx context.Context, run model.WorkerRun) (model.WorkerClaimResult, error) {
	workerID, err := scheduleID()
	if err != nil {
		return model.WorkerClaimResult{}, err
	}
	result, err := worker.repository.CommitClaim(ctx, model.WorkerClaimCommand{
		Run: run, WorkerID: workerID, Now: worker.now().UnixMilli(),
	})
	if err != nil {
		return result, fmt.Errorf("claim metadata execution: %w", err)
	}
	return result, nil
}

func (worker *Worker) Recover(ctx context.Context) ([]string, error) {
	ids, err := worker.repository.Recoverable(ctx, worker.now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("recover metadata executions: %w", err)
	}
	return ids, nil
}
