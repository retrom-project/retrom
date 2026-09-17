package metadatascrape

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/metadatascrape"
)

func (worker *MediaWorker) settle(parent context.Context, execution mediaExecution,
	publication model.AssetPublication, code string, failed bool,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	retryable := true
	if failed {
		retryable = mediaRetryable(code)
	}
	result, err := worker.repository.CommitSettle(ctx, model.MediaSettleCommand{
		Claim:       execution.Claim,
		Publication: publication,
		Code:        code,
		Failed:      failed,
		Retryable:   retryable,
		Now:         worker.now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("settle media fetch: %w", err)
	}
	if result.State == "CANCELLED" {
		return context.Canceled
	}
	return nil
}

func (worker *MediaWorker) heartbeat(
	ctx context.Context, cancel context.CancelCauseFunc, claim model.MediaClaim, stopped chan<- struct{},
) {
	defer close(stopped)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.refresh(ctx, claim); err != nil {
				cancel(err)
				return
			}
		}
	}
}

func (worker *MediaWorker) refresh(ctx context.Context, claim model.MediaClaim) error {
	err := worker.repository.CommitRefresh(ctx, model.MediaRefreshCommand{
		Claim: claim,
		Now:   worker.now().UnixMilli(),
	})
	return mediaError("refresh media ownership", err)
}
