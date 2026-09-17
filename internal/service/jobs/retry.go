package jobs

import (
	"context"
	"fmt"

	model "retrom/internal/model/jobs"

	"github.com/google/uuid"
)

func (service *Service) Retry(
	ctx context.Context, jobID string, expectedVersion int64,
) (model.Result, error) {
	executionID, err := uuid.NewV7()
	if err != nil {
		return model.Result{}, fmt.Errorf("jobs/retry execution ID: %w", err)
	}
	result, err := service.repository.CommitRetry(ctx, model.RetryCommand{
		JobID: jobID, ExecutionID: executionID.String(),
		ExpectedVersion: expectedVersion,
		NowMS:           service.now().UnixMilli(),
	})
	if err != nil {
		return model.Result{}, fmt.Errorf("jobs/retry: %w", err)
	}
	return result, nil
}
