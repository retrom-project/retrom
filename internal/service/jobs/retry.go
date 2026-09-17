package jobs

import (
	"context"
	"fmt"
	model "retrom/internal/model/jobs"
)

func (service *Service) Retry(
	ctx context.Context, jobID string, expectedVersion int64,
) (model.Result, error) {
	result, err := service.repository.CommitRetry(ctx, model.RetryCommand{
		JobID: jobID, ExpectedVersion: expectedVersion,
		NowMS: service.now().UnixMilli(),
	})
	if err != nil {
		return model.Result{}, fmt.Errorf("jobs/retry: %w", err)
	}
	return result, nil
}
