package metadatascrape

import (
	"context"
	"errors"
	"time"

	"retrom/internal/adapter/metadata/hasheous"
)

func (worker *MediaWorker) claim(ctx context.Context, id string) (mediaExecution, error) {
	workerID, err := scheduleID()
	if err != nil {
		return mediaExecution{}, err
	}
	var execution mediaExecution
	err = worker.repository.WithWrite(ctx, func(scope MediaScope) error {
		var err error
		execution, err = worker.claimInScope(ctx, scope, id, workerID)
		return err
	})
	return execution, mediaError("claim media transaction", err)
}

func (worker *MediaWorker) claimInScope(
	ctx context.Context, scope MediaScope, id, workerID string,
) (mediaExecution, error) {
	snapshot, err := scope.Read.Snapshot(ctx, id)
	if err != nil {
		return mediaExecution{}, mediaError("read media claim", err)
	}
	now := worker.now().UnixMilli()
	execution := mediaExecution{Claim: MediaClaim{
		JobID: id, WorkerID: workerID, Execution: snapshot.Job.Execution,
		Attempt: snapshot.Job.Attempt, Version: snapshot.Job.Version, Now: now, Deadline: snapshot.Job.Deadline,
	}, Asset: snapshot.Asset}
	if snapshot.Job.State == "CANCELLED" {
		return execution, reconcileCancelledMedia(ctx, scope, snapshot, now)
	}
	if !mediaClaimable(snapshot.Job, now) {
		return execution, nil
	}
	execution.Code, execution.Cause = mediaTerminalCause(snapshot, now)
	execution.Claim.Terminal = execution.Cause != nil
	if execution.Claim.Terminal {
		err := execution.acquire(ctx, scope)
		return execution, err
	}
	if snapshot.Job.State == "RUNNING" {
		available := min(now+metadataRetryDelay(snapshot.Job.Attempt), snapshot.Job.Deadline)
		return execution, mediaError("requeue expired media lease", scope.Leases.Requeue(ctx, execution.Claim, available))
	}
	if snapshot.RunState == "RUNNING" || snapshot.Job.AvailableAt > now {
		return execution, nil
	}
	if err := execution.prepare(ctx, scope, snapshot); err != nil {
		return execution, err
	}
	return execution, nil
}

func (execution *mediaExecution) prepare(ctx context.Context, scope MediaScope, snapshot MediaSnapshot) error {
	snapshot, err := freezeMediaOrder(ctx, scope, snapshot, execution.Claim.Now)
	if err != nil {
		return err
	}
	if !snapshot.First {
		return nil
	}
	running, err := scope.Read.Running(ctx, execution.Claim.Now)
	if err != nil {
		return mediaError("read active media capacity", err)
	}
	if running >= 2 {
		return nil
	}
	executing, err := scope.Read.RunExecuting(ctx, snapshot.Asset.RunID, execution.Claim.Now)
	if err != nil {
		return mediaError("read media run capacity", err)
	}
	if executing {
		return nil
	}
	execution.Asset = snapshot.Asset
	execution.Limit = min(hasheous.MaximumAssetReadBytes, MediaRunBudget-snapshot.Charged)
	if execution.Limit <= 0 {
		execution.Code = "ASSET_RUN_BUDGET_EXCEEDED"
		execution.Cause = hasheous.ErrAssetReadLimit
		execution.Claim.Terminal = true
	} else {
		execution.Claim.Attempt++
		if execution.Claim.Deadline == 0 {
			execution.Claim.Deadline = execution.Claim.Now + (30 * time.Minute).Milliseconds()
		}
	}
	return execution.acquire(ctx, scope)
}

func (execution *mediaExecution) acquire(ctx context.Context, scope MediaScope) error {
	if err := scope.Leases.Claim(ctx, execution.Claim); err != nil {
		return mediaError("claim media lease", err)
	}
	if !execution.Claim.Terminal {
		if err := scope.Assets.Reserve(ctx, execution.Asset, execution.Limit, execution.Claim.Now); err != nil {
			return mediaError("reserve media bytes", err)
		}
	}
	execution.Acquired = true
	return nil
}

func mediaClaimable(job MediaJob, now int64) bool {
	switch job.State {
	case "QUEUED":
		return true
	case "RUNNING", "CANCEL_REQUESTED":
		return job.LeaseUntil <= now || job.Deadline <= now
	default:
		return false
	}
}

func mediaTerminalCause(snapshot MediaSnapshot, now int64) (string, error) {
	job := snapshot.Job
	switch {
	case job.State == "CANCEL_REQUESTED":
		return "MEDIA_CANCELLED", context.Canceled
	case !mediaOwnerAvailable(snapshot):
		return "MEDIA_OWNER_UNAVAILABLE", ErrGameDeleted
	case job.Deadline > 0 && job.Deadline <= now:
		return "MEDIA_EXECUTION_EXPIRED", context.DeadlineExceeded
	case job.Attempt >= job.MaxAttempts:
		return "MEDIA_ATTEMPTS_EXHAUSTED", ErrAttemptsExhausted
	}
	if err := validateMediaInput(snapshot); err != nil {
		return "MEDIA_INPUT_INVALID", err
	}
	if snapshot.Asset.Status == "READY" || snapshot.Asset.Status == "CANCELLED" {
		return "MEDIA_ASSET_STATE_INVALID", ErrAssetStateConflict
	}
	return "", nil
}

func freezeMediaOrder(ctx context.Context, scope MediaScope, snapshot MediaSnapshot, now int64) (MediaSnapshot, error) {
	if snapshot.Frozen {
		return snapshot, nil
	}
	assets, err := scope.Read.Ordering(ctx, snapshot.Asset.RunID)
	if err != nil {
		return snapshot, mediaError("freeze media order", err)
	}
	sortMedia(assets)
	if err := scope.Assets.Freeze(ctx, snapshot.Asset.RunID, assets, now); err != nil {
		return snapshot, mediaError("freeze media order", err)
	}
	current, err := scope.Read.Snapshot(ctx, snapshot.Job.ID)
	return current, mediaError("read frozen media order", err)
}

func reconcileCancelledMedia(ctx context.Context, scope MediaScope, snapshot MediaSnapshot, now int64) error {
	asset := snapshot.Asset
	if asset.ID == "" || asset.Status == "READY" || asset.Status == "CANCELLED" {
		return nil
	}
	if err := scope.Assets.Fail(ctx, asset, "CANCELLED", "MEDIA_CANCELLED", now); err != nil {
		return errors.Join(context.Canceled, err)
	}
	return nil
}
