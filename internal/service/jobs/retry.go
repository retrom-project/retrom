package jobs

import (
	"context"
	"fmt"
)

func (service *Service) Retry(
	ctx context.Context, jobID string, expectedVersion int64,
) (Result, error) {
	result, err := service.repository.CommitRetry(ctx, RetryCommand{
		JobID: jobID, ExpectedVersion: expectedVersion,
		NowMS: service.now().UnixMilli(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("jobs/retry: %w", err)
	}
	return result, nil
}
