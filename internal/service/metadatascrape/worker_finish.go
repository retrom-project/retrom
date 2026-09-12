package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (worker *Worker) settle(parent context.Context, claim WorkerClaim, count int, code string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	err := worker.repository.WithWrite(ctx, func(scope WorkerScope) error {
		now := worker.now().UnixMilli()
		status, err := scope.Leases.Status(ctx, claim, now)
		if err != nil {
			return fmt.Errorf("read metadata completion ownership: %w", err)
		}
		if status.State == "" {
			return nil
		}
		outcome := WorkerOutcome{Claim: claim, State: "SUCCEEDED", RunState: "COMPLETED", Count: count, Now: now}
		initial := NewInitialReview(scope.Initial)
		switch {
		case status.State == "CANCEL_REQUESTED" || status.State == "CANCELLED":
			outcome.State = "CANCELLED"
			outcome.RunState = "CANCELLED"
			err = initial.Cancel(ctx, claim.RunID, status.ParentCancelled, now)
		case status.State != "RUNNING":
			return nil
		case cause != nil || status.Expired:
			if cause == nil {
				cause = context.DeadlineExceeded
				code = "METADATA_EXECUTION_EXPIRED"
			}
			outcome.State = "FAILED"
			outcome.RunState = "FAILED"
			outcome.Code = code
			err = initial.Fail(ctx, claim.RunID, code, now)
		default:
			err = initial.Complete(ctx, claim.RunID, now)
		}
		if err != nil {
			return err
		}
		return scope.Write.Finish(ctx, outcome)
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("finish metadata execution: %w", err))
	}
	return cause
}
