package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/metadatascrape"
)

func (worker *Worker) settle(
	parent context.Context,
	claim model.WorkerClaim,
	count int,
	code string,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	result, err := worker.repository.CommitSettle(ctx, model.WorkerSettleCommand{
		Claim: claim, Count: count, Code: code,
		Failed: cause != nil, Now: worker.now().UnixMilli(),
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("finish metadata execution: %w", err))
	}
	if result.Expired && cause == nil {
		cause = context.DeadlineExceeded
	}
	return cause
}
