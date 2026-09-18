package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
)

type (
	MediaRepository struct {
		database      *sql.DB
		preCommitHook func() error
	}
	mediaRecords struct{ executor dbexec.Executor }
)

func NewMedia(database *sql.DB) *MediaRepository { return &MediaRepository{database: database} }

func WithMediaPreCommitHook(repo *MediaRepository, hook func() error) {
	repo.preCommitHook = hook
}

func (repository *MediaRepository) LoadMediaSnapshot(
	ctx context.Context, id string,
) (metadatascrape.MediaSnapshot, error) {
	return (mediaRecords{executor: repository.database}).Snapshot(ctx, id)
}

func (repository *MediaRepository) CommitClaim(
	ctx context.Context, cmd metadatascrape.MediaClaimCommand,
) (metadatascrape.MediaClaimResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return metadatascrape.MediaClaimResult{}, fmt.Errorf("begin media claim: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := mediaRecords{tx}
	result, err := claimMedia(ctx, records, cmd)
	if err != nil {
		return metadatascrape.MediaClaimResult{}, err
	}
	if err := mediaCommitWithHook(tx, repository.preCommitHook); err != nil {
		return metadatascrape.MediaClaimResult{}, err
	}
	return result, nil
}

func claimMedia(
	ctx context.Context, records mediaRecords, cmd metadatascrape.MediaClaimCommand,
) (metadatascrape.MediaClaimResult, error) {
	snapshot, err := records.Snapshot(ctx, cmd.JobID)
	if err != nil {
		return metadatascrape.MediaClaimResult{}, mediaErr("read media claim", err)
	}
	now := cmd.Now
	result := metadatascrape.MediaClaimResult{
		Claim: metadatascrape.MediaClaim{
			JobID: cmd.JobID, WorkerID: cmd.WorkerID, Execution: snapshot.Job.Execution,
			Attempt: snapshot.Job.Attempt, Version: snapshot.Job.Version, Now: now,
			Deadline: snapshot.Job.Deadline,
		},
		Asset: snapshot.Asset,
	}
	if snapshot.Job.State == "CANCELLED" {
		return result, reconcileCancelledAsset(ctx, records, snapshot, now)
	}
	if !metadatascrape.MediaClaimable(snapshot.Job, now) {
		return result, nil
	}
	code, cause := metadatascrape.MediaTerminalCause(snapshot, now)
	result.Code = code
	result.Failed = cause != nil
	result.Claim.Terminal = cause != nil
	if result.Claim.Terminal {
		return acquireMedia(ctx, records, result)
	}
	if snapshot.Job.State == "RUNNING" {
		available := min(now+metadatascrape.MetadataRetryDelay(snapshot.Job.Attempt), snapshot.Job.Deadline)
		return result, mediaErr("requeue expired media lease", records.Requeue(ctx, result.Claim, available))
	}
	if snapshot.RunState == "RUNNING" || snapshot.Job.AvailableAt > now {
		return result, nil
	}
	return prepareMedia(ctx, records, result, snapshot)
}

func prepareMedia(
	ctx context.Context,
	records mediaRecords,
	result metadatascrape.MediaClaimResult,
	snapshot metadatascrape.MediaSnapshot,
) (metadatascrape.MediaClaimResult, error) {
	snapshot, err := freezeMediaAssetOrder(ctx, records, snapshot, result.Claim.Now)
	if err != nil {
		return result, err
	}
	if !snapshot.First {
		return result, nil
	}
	running, err := records.Running(ctx, result.Claim.Now)
	if err != nil {
		return result, mediaErr("read active media capacity", err)
	}
	if running >= 2 {
		return result, nil
	}
	executing, err := records.RunExecuting(ctx, snapshot.Asset.RunID, result.Claim.Now)
	if err != nil {
		return result, mediaErr("read media run capacity", err)
	}
	if executing {
		return result, nil
	}
	result.Asset = snapshot.Asset
	result.Limit = min(hasheous.MaximumAssetReadBytes, metadatascrape.MediaRunBudget-snapshot.Charged)
	if result.Limit <= 0 {
		result.Code = "ASSET_RUN_BUDGET_EXCEEDED"
		result.Failed = true
		result.Claim.Terminal = true
	} else {
		result.Claim.Attempt++
		if result.Claim.Deadline == 0 {
			result.Claim.Deadline = result.Claim.Now + (30 * time.Minute).Milliseconds()
		}
	}
	return acquireMedia(ctx, records, result)
}

func acquireMedia(
	ctx context.Context, records mediaRecords, result metadatascrape.MediaClaimResult,
) (metadatascrape.MediaClaimResult, error) {
	if err := records.Claim(ctx, result.Claim); err != nil {
		return result, mediaErr("claim media lease", err)
	}
	if !result.Claim.Terminal {
		if err := records.Reserve(ctx, result.Asset, result.Limit, result.Claim.Now); err != nil {
			return result, mediaErr("reserve media bytes", err)
		}
	}
	result.Acquired = true
	return result, nil
}

func freezeMediaAssetOrder(
	ctx context.Context, records mediaRecords, snapshot metadatascrape.MediaSnapshot, now int64,
) (metadatascrape.MediaSnapshot, error) {
	if snapshot.Frozen {
		return snapshot, nil
	}
	assets, err := records.Ordering(ctx, snapshot.Asset.RunID)
	if err != nil {
		return snapshot, mediaErr("freeze media order", err)
	}
	metadatascrape.SortMedia(assets)
	if err := records.Freeze(ctx, snapshot.Asset.RunID, assets, now); err != nil {
		return snapshot, mediaErr("freeze media order", err)
	}
	current, err := records.Snapshot(ctx, snapshot.Job.ID)
	return current, mediaErr("read frozen media order", err)
}

func reconcileCancelledAsset(
	ctx context.Context, records mediaRecords, snapshot metadatascrape.MediaSnapshot, now int64,
) error {
	asset := snapshot.Asset
	if asset.ID == "" || asset.Status == "READY" || asset.Status == "CANCELLED" {
		return nil
	}
	if err := records.Fail(ctx, asset, "CANCELLED", "MEDIA_CANCELLED", now); err != nil {
		return errors.Join(context.Canceled, err)
	}
	return nil
}

func (repository *MediaRepository) CommitRefresh(
	ctx context.Context, cmd metadatascrape.MediaRefreshCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media refresh: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := mediaRecords{tx}
	snapshot, err := records.Snapshot(ctx, cmd.Claim.JobID)
	if err != nil {
		return mediaErr("read media lease", err)
	}
	if err := metadatascrape.MediaActive(snapshot, cmd.Claim, cmd.Now); err != nil {
		return fmt.Errorf("validate media lease: %w", err)
	}
	if err := records.Refresh(ctx, cmd.Claim, cmd.Now); err != nil {
		return mediaErr("renew media lease", err)
	}
	return mediaCommitWithHook(tx, repository.preCommitHook)
}

func (repository *MediaRepository) CommitSettle(
	ctx context.Context, cmd metadatascrape.MediaSettleCommand,
) (metadatascrape.MediaSettleResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return metadatascrape.MediaSettleResult{}, fmt.Errorf("begin media settle: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := mediaRecords{tx}
	snapshot, err := records.Snapshot(ctx, cmd.Claim.JobID)
	if err != nil {
		return metadatascrape.MediaSettleResult{}, mediaErr("read media completion", err)
	}
	if !metadatascrape.MediaOwned(snapshot, cmd.Claim) {
		return metadatascrape.MediaSettleResult{}, metadatascrape.ErrExecutionLost
	}
	if snapshot.Job.State != "RUNNING" && snapshot.Job.State != "QUEUED" && snapshot.Job.State != "CANCEL_REQUESTED" {
		return metadatascrape.MediaSettleResult{}, metadatascrape.ErrExecutionLost
	}
	outcome := metadatascrape.MediaCompletion(snapshot, cmd)
	if err := publishMediaResult(ctx, records, snapshot, outcome, cmd.Publication); err != nil {
		return metadatascrape.MediaSettleResult{}, err
	}
	if err := records.Finish(ctx, outcome); err != nil {
		return metadatascrape.MediaSettleResult{}, mediaErr("finish media job", err)
	}
	if err := mediaCommitWithHook(tx, repository.preCommitHook); err != nil {
		return metadatascrape.MediaSettleResult{}, err
	}
	return metadatascrape.MediaSettleResult{State: outcome.State}, nil
}

func publishMediaResult(
	ctx context.Context, records mediaRecords, snapshot metadatascrape.MediaSnapshot,
	outcome metadatascrape.MediaOutcome, publication metadatascrape.AssetPublication,
) error {
	if outcome.State == "SUCCEEDED" {
		publication.Now = outcome.Now
		return mediaErr("publish ready media", records.Publish(ctx, publication, snapshot.Asset.Version))
	}
	if snapshot.Asset.ID == "" || snapshot.Asset.Status == "READY" {
		return nil
	}
	return mediaErr("close media asset", records.Fail(ctx, snapshot.Asset, outcome.State, outcome.Code, outcome.Now))
}

func (repository *MediaRepository) CommitAccount(
	ctx context.Context, cmd metadatascrape.MediaAccountCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media account: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := mediaRecords{tx}
	snapshot, err := records.Snapshot(ctx, cmd.Claim.JobID)
	if err != nil {
		return mediaErr("read media byte owner", err)
	}
	if !metadatascrape.MediaOwned(snapshot, cmd.Claim) || snapshot.Asset.Reserved != cmd.Limit {
		return metadatascrape.ErrExecutionLost
	}
	if err := metadatascrape.ValidateMediaInput(snapshot); err != nil {
		return fmt.Errorf("validate media input: %w", err)
	}
	if err := records.Account(ctx, snapshot.Asset, cmd.Received, cmd.Now); err != nil {
		return mediaErr("account media response", err)
	}
	return mediaCommitWithHook(tx, repository.preCommitHook)
}

func mediaCommitWithHook(tx *sql.Tx, hook func() error) error {
	if hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media transaction: %w", err)
	}
	return nil
}

func mediaErr(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, cause)
}

func mediaChanged(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write media record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count media records: %w", err)
	}
	if count != 1 {
		return metadatascrape.ErrExecutionLost
	}
	return nil
}

func (records mediaRecords) event(ctx context.Context, id, kind, data string, now int64) error {
	return mediaChanged(records.executor.ExecContext(ctx, `
INSERT INTO job_events
 (job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 SELECT id,scope_type,scope_id,?,?,? FROM jobs WHERE id=? AND kind='MEDIA_FETCH'`, kind, data, now, id))
}
