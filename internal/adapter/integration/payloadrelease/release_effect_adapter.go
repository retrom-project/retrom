package payloadrelease

import (
	"context"
	"fmt"
	"time"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
)

type releaseEffectWaiter struct{ service *Service }

func (waiter releaseEffectWaiter) Wait(ctx context.Context, duration time.Duration) error {
	if err := waiter.service.waitFor(ctx, duration); err != nil {
		return fmt.Errorf("wait for payload mutation: %w", err)
	}
	return nil
}

func (service *Service) releaseEffect(ctx context.Context, job claimedJob) error {
	if err := service.effects.Execute(ctx, payloadreleasemodel.Execution{Work: job.Work, Input: job.Input}); err != nil {
		return fmt.Errorf("release payload effect: %w", err)
	}
	return nil
}
