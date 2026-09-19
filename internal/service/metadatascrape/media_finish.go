package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"time"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

func (worker *MediaWorker) settle(parent context.Context, execution mediaExecution,
	publication metadatascrapemodel.AssetPublication, code string, cause error,
) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	err := worker.repository.WithWrite(ctx, func(scope metadatascrapemodel.MediaScope) error {
		snapshot, err := scope.Read.Snapshot(ctx, execution.Claim.JobID)
		if err != nil {
			return mediaError("read media completion", err)
		}
		if !mediaOwned(snapshot, execution.Claim) {
			return metadatascrapemodel.ErrExecutionLost
		}
		if snapshot.Job.State != "RUNNING" && snapshot.Job.State != "QUEUED" && snapshot.Job.State != "CANCEL_REQUESTED" {
			return metadatascrapemodel.ErrExecutionLost
		}
		outcome, nextCause := mediaCompletion(snapshot, execution.Claim, code, cause, worker.now().UnixMilli())
		cause = nextCause
		if err := publishMediaOutcome(ctx, scope, snapshot, outcome, publication); err != nil {
			return err
		}
		return mediaError("finish media job", scope.Leases.Finish(ctx, outcome))
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("settle media fetch: %w", err))
	}
	return cause
}

func mediaCompletion(
	snapshot metadatascrapemodel.MediaSnapshot, claim metadatascrapemodel.MediaClaim, code string, cause error, now int64,
) (metadatascrapemodel.MediaOutcome, error) {
	outcome := metadatascrapemodel.MediaOutcome{Claim: claim, State: "SUCCEEDED", Now: now}
	if snapshot.Job.State == "CANCEL_REQUESTED" || !mediaOwnerAvailable(snapshot) {
		outcome.State = "CANCELLED"
		outcome.Code = "MEDIA_CANCELLED"
		return outcome, errors.Join(cause, context.Canceled)
	}
	if cause == nil {
		cause = mediaActive(snapshot, claim, now)
	}
	if cause != nil {
		outcome.State = "FAILED"
		outcome.Code = code
		outcome.Retryable = mediaRetryable(cause)
		if outcome.Code == "" {
			outcome.Code = "MEDIA_EXECUTION_INTERRUPTED"
		}
		if snapshot.Job.Deadline > 0 && snapshot.Job.Deadline <= now {
			outcome.Code = "MEDIA_EXECUTION_EXPIRED"
			cause = errors.Join(cause, context.DeadlineExceeded)
		}
	}
	return outcome, cause
}

func publishMediaOutcome(
	ctx context.Context,
	scope metadatascrapemodel.MediaScope,
	snapshot metadatascrapemodel.MediaSnapshot,
	outcome metadatascrapemodel.MediaOutcome,
	publication metadatascrapemodel.AssetPublication,
) error {
	if outcome.State == "SUCCEEDED" {
		publication.Now = outcome.Now
		return mediaError("publish ready media", scope.Assets.Publish(ctx, publication, snapshot.Asset.Version))
	}
	if snapshot.Asset.ID == "" || snapshot.Asset.Status == "READY" {
		return nil
	}
	err := scope.Assets.Fail(ctx, snapshot.Asset, outcome.State, outcome.Code, outcome.Now)
	return mediaError("close media asset", err)
}

func (worker *MediaWorker) heartbeat(
	ctx context.Context, cancel context.CancelCauseFunc, claim metadatascrapemodel.MediaClaim, stopped chan<- struct{},
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

func (worker *MediaWorker) refresh(ctx context.Context, claim metadatascrapemodel.MediaClaim) error {
	err := worker.repository.WithWrite(ctx, func(scope metadatascrapemodel.MediaScope) error {
		snapshot, err := scope.Read.Snapshot(ctx, claim.JobID)
		if err != nil {
			return mediaError("read media lease", err)
		}
		now := worker.now().UnixMilli()
		if err := mediaActive(snapshot, claim, now); err != nil {
			return err
		}
		return mediaError("renew media lease", scope.Leases.Refresh(ctx, claim, now))
	})
	return mediaError("refresh media ownership", err)
}
