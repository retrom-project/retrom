package metadatascrape

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/hasheous"
)

type pendingAsset struct {
	id  string
	ref hasheous.AssetRef
}

func (service *Service) fetchPendingAssets(ctx context.Context, runID string) error {
	assets, err := service.pendingAssets(ctx, runID)
	if err != nil {
		return err
	}
	var consumed int64
	for _, asset := range assets {
		consumed, err = service.fetchPendingAsset(ctx, asset, consumed)
		if err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) pendingAssets(ctx context.Context, runID string) ([]pendingAsset, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT a.id,
a.provider_asset_id,
a.kind_hint,
a.ordinal,
a.source_path
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
WHERE c.scrape_run_id=?
AND a.status='PENDING'
ORDER BY (SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id) DESC,
  (SELECT min(e.query_order)
FROM scrape_candidate_hits h
JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id
WHERE h.scrape_candidate_id=c.id),
  c.provider_game_id,
a.kind_hint,
a.ordinal,
a.id
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]pendingAsset, 0)
	for rows.Next() {
		var asset pendingAsset
		if err := rows.Scan(
			&asset.id,
			&asset.ref.ProviderAssetID,
			&asset.ref.Kind,
			&asset.ref.Ordinal,
			&asset.ref.Path,
		); err != nil {
			return nil, fmt.Errorf("metadatascrape/service: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	return assets, nil
}

func (service *Service) fetchPendingAsset(
	ctx context.Context,
	asset pendingAsset,
	consumed int64,
) (int64, error) {
	if consumed >= 100<<20 {
		return consumed, service.markAssetFailed(ctx, asset.id, "ASSET_RUN_BUDGET_EXCEEDED")
	}
	data, err := service.provider.FetchAsset(ctx, asset.ref)
	if err != nil {
		return consumed, service.markAssetFailed(ctx, asset.id, stableAssetError(err))
	}
	consumed += int64(len(data.Bytes))
	if consumed > 100<<20 {
		return consumed, service.markAssetFailed(ctx, asset.id, "ASSET_RUN_BUDGET_EXCEEDED")
	}
	metadata, err := service.blobs.Put(bytes.NewReader(data.Bytes))
	if err != nil {
		return consumed, fmt.Errorf("metadatascrape/service: %w", err)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return consumed, fmt.Errorf("metadatascrape/service: %w", err)
	}
	blobID, registerErr := blobstore.EnsureRecord(
		ctx,
		transaction,
		metadata,
		data.MediaType,
		service.now().UnixMilli(),
	)
	var updated int64
	if registerErr == nil {
		var updateResult interface{ RowsAffected() (int64, error) }
		updateResult, registerErr = transaction.ExecContext(
			ctx,
			`
UPDATE scrape_candidate_assets
SET status='READY',
blob_id=?,
width_px=?,
height_px=?,
media_type=?,
fetched_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND status='PENDING'
AND EXISTS(
  SELECT 1 FROM scrape_candidates candidate
  JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
  LEFT JOIN games game ON game.id=run.game_id
  WHERE candidate.id=scrape_candidate_assets.scrape_candidate_id
    AND (run.game_id IS NULL OR game.status='PUBLISHED')
)
`,
			blobID,
			data.Width,
			data.Height,
			data.MediaType,
			service.now().UnixMilli(),
			service.now().UnixMilli(),
			asset.id,
		)
		if registerErr == nil {
			updated, registerErr = updateResult.RowsAffected()
		}
	}
	if registerErr == nil {
		if updated != 1 {
			registerErr = errGameDeleted
		} else {
			registerErr = transaction.Commit()
		}
	} else {
		cleanup.Rollback(transaction)
	}
	if registerErr != nil {
		return consumed, fmt.Errorf("metadatascrape/service: %w", registerErr)
	}
	return consumed, nil
}

func (service *Service) markAssetFailed(ctx context.Context, assetID, code string) error {
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(
		ctx,
		`
UPDATE scrape_candidate_assets
SET status='FAILED',
error_code=?,
fetched_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND status='PENDING'
`,
		code,
		now,
		now,
		assetID,
	)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errAssetStateConflict
	}
	return nil
}

func stableAssetError(err error) string {
	for _, known := range []error{
		hasheous.ErrAssetURLInvalid,
		hasheous.ErrAssetNetwork,
		hasheous.ErrAssetRedirectLimit,
		hasheous.ErrAssetHTTPStatus,
		hasheous.ErrAssetTooLarge,
		hasheous.ErrAssetURLRejected,
		hasheous.ErrAssetDNSFailed,
		hasheous.ErrAssetIPRejected,
		hasheous.ErrAssetMediaTypeInvalid,
		hasheous.ErrAssetMediaTypeMismatch,
		hasheous.ErrAssetDecodeFailed,
		hasheous.ErrAssetPixelLimit,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "ASSET_FETCH_FAILED"
}
