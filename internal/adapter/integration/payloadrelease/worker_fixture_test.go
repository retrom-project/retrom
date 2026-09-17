package payloadrelease

import (
	"context"
	"fmt"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
	"time"
)

func (service *Service) claim(ctx context.Context) (claimedJob, bool, error) {
	work, found, err := service.worker.Claim(ctx)
	if err != nil {
		return claimedJob{}, false, fmt.Errorf("claim release worker: %w", err)
	}
	if !found {
		return claimedJob{}, false, nil
	}
	input, err := payloadreleaseservice.DecodeWork(work)
	if err != nil {
		return claimedJob{}, false, fmt.Errorf("decode claimed release: %w", err)
	}
	return claimedWork(payloadreleasemodel.Execution{Work: work, Input: input}), true, nil
}

func (service *Service) finish(ctx context.Context, job claimedJob, executionErr error) error {
	if err := service.worker.Finish(ctx, job.Work, executionErr); err != nil {
		return fmt.Errorf("finish release worker: %w", err)
	}
	return nil
}
func releaseRetryDelay(attempt int64) time.Duration { return payloadreleaseservice.RetryDelay(attempt) }
