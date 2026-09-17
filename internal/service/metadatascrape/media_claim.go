package metadatascrape

import (
	"context"

	model "retrom/internal/model/metadatascrape"
)

func (worker *MediaWorker) claim(ctx context.Context, id string) (mediaExecution, error) {
	workerID, err := scheduleID()
	if err != nil {
		return mediaExecution{}, err
	}
	result, err := worker.repository.CommitClaim(ctx, model.MediaClaimCommand{
		JobID: id, WorkerID: workerID, Now: worker.now().UnixMilli(),
	})
	if err != nil {
		return mediaExecution{}, mediaError("claim media transaction", err)
	}
	return mediaExecution{
		Claim: result.Claim, Asset: result.Asset, Limit: result.Limit,
		Acquired: result.Acquired, Code: result.Code, Failed: result.Failed,
	}, nil
}
