package metadatascrape

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type MediaWorker struct {
	repository metadatascrapemodel.MediaRepository
	provider   metadatascrapemodel.MediaProvider
	blobs      metadatascrapemodel.AssetBlobs
	now        func() time.Time
}
type mediaExecution struct {
	Claim    metadatascrapemodel.MediaClaim
	Asset    metadatascrapemodel.MediaAsset
	Limit    int64
	Acquired bool
	Code     string
	Cause    error
}

func NewMediaWorker(
	repository metadatascrapemodel.MediaRepository,
	provider metadatascrapemodel.MediaProvider,
	blobs metadatascrapemodel.AssetBlobs,
	now func() time.Time,
) *MediaWorker {
	return &MediaWorker{repository: repository, provider: provider, blobs: blobs, now: now}
}

func (worker *MediaWorker) Recover(ctx context.Context) ([]string, error) {
	ids, err := worker.repository.Recoverable(ctx, worker.now().UnixMilli())
	return ids, mediaError("recover media queue", err)
}

func (worker *MediaWorker) Run(parent context.Context, id string) error {
	execution, err := worker.claim(parent, id)
	if err != nil {
		return fmt.Errorf("claim media fetch: %w", err)
	}
	if !execution.Acquired {
		return nil
	}
	if execution.Claim.Terminal {
		return worker.settle(parent, execution, metadatascrapemodel.AssetPublication{}, execution.Code, execution.Cause)
	}
	remaining := time.Duration(execution.Claim.Deadline-worker.now().UnixMilli()) * time.Millisecond
	ctx, timeout := context.WithTimeout(parent, remaining)
	defer timeout()
	ctx, cancel := context.WithCancelCause(ctx)
	stopped := make(chan struct{})
	go worker.heartbeat(ctx, cancel, execution.Claim, stopped)
	defer func() { cancel(nil); <-stopped }()
	publication, code, cause := worker.fetch(ctx, execution)
	cause = errors.Join(cause, context.Cause(ctx))
	if code == "" && cause != nil {
		code = "MEDIA_EXECUTION_INTERRUPTED"
	}
	return worker.settle(parent, execution, publication, code, cause)
}

func (worker *MediaWorker) fetch(
	ctx context.Context,
	execution mediaExecution,
) (metadatascrapemodel.AssetPublication, string, error) {
	var data metadatamodel.AssetData
	cause := context.Cause(ctx)
	if cause == nil {
		data, cause = worker.provider.FetchAssetBounded(ctx, execution.Asset.Reference, execution.Limit)
	}
	if err := worker.account(ctx, execution, data.ReceivedBytes); err != nil {
		return metadatascrapemodel.AssetPublication{}, "MEDIA_ACCOUNT_FAILED", errors.Join(cause, err)
	}
	cause = errors.Join(cause, context.Cause(ctx))
	if cause != nil {
		return metadatascrapemodel.AssetPublication{}, mediaErrorCode(cause), cause
	}
	blob, err := worker.blobs.Put(bytes.NewReader(data.Bytes))
	if err != nil {
		return metadatascrapemodel.AssetPublication{}, "MEDIA_BLOB_FAILED", fmt.Errorf("store media bytes: %w", err)
	}
	publication := metadatascrapemodel.AssetPublication{
		ID: execution.Asset.ID, Blob: blob, MediaType: data.MediaType,
		Width: data.Width, Height: data.Height,
	}
	return publication, "", nil
}

func mediaErrorCode(cause error) string {
	if errors.Is(cause, metadatamodel.ErrAssetReadLimit) {
		return "ASSET_RUN_BUDGET_EXCEEDED"
	}
	return stableAssetError(cause)
}

func (worker *MediaWorker) account(parent context.Context, execution mediaExecution, received int64) error {
	if received < 0 || received > execution.Limit {
		return metadatascrapemodel.ErrMediaBudget
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	err := worker.repository.WithWrite(ctx, func(scope metadatascrapemodel.MediaScope) error {
		snapshot, err := scope.Read.Snapshot(ctx, execution.Claim.JobID)
		if err != nil {
			return mediaError("read media byte owner", err)
		}
		if !mediaOwned(snapshot, execution.Claim) || snapshot.Asset.Reserved != execution.Limit {
			return metadatascrapemodel.ErrExecutionLost
		}
		if err := validateMediaInput(snapshot); err != nil {
			return err
		}
		err = scope.Assets.Account(ctx, snapshot.Asset, received, worker.now().UnixMilli())
		return mediaError("account media response", err)
	})
	return mediaError("settle known media bytes", err)
}

func mediaError(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, cause)
}
