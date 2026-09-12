package metadatascrape

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/hasheous"
)

var (
	ErrAssetStateConflict = errors.New("ASSET_STATE_CONFLICT")
	ErrGameDeleted        = errors.New("METADATA_GAME_DELETED")
)

type PendingAsset struct {
	ID        string
	Reference hasheous.AssetRef
}
type AssetPublication struct {
	ID            string
	Blob          blobstore.Metadata
	MediaType     string
	Width, Height int
	Now           int64
}
type AssetRecords interface {
	Pending(context.Context, string) ([]PendingAsset, error)
	Publish(context.Context, AssetPublication) error
	Fail(context.Context, string, string, int64) error
}
type AssetProvider interface {
	FetchAsset(context.Context, hasheous.AssetRef) (hasheous.AssetData, error)
}
type AssetBlobs interface {
	Put(io.Reader) (blobstore.Metadata, error)
}
type AssetService struct {
	records  AssetRecords
	provider AssetProvider
	blobs    AssetBlobs
	now      func() time.Time
}

func NewAssets(records AssetRecords, provider AssetProvider, blobs AssetBlobs, now func() time.Time) *AssetService {
	return &AssetService{records: records, provider: provider, blobs: blobs, now: now}
}

func (service *AssetService) Run(ctx context.Context, runID string) error {
	assets, err := service.records.Pending(ctx, runID)
	if err != nil {
		return fmt.Errorf("load pending metadata assets: %w", err)
	}
	var consumed int64
	for _, asset := range assets {
		consumed, err = service.fetch(ctx, asset, consumed)
		if err != nil {
			return err
		}
	}
	return nil
}

func (service *AssetService) fetch(ctx context.Context, asset PendingAsset, consumed int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return consumed, fmt.Errorf("fetch metadata asset: %w", err)
	}
	if consumed >= 100<<20 {
		return consumed, service.fail(ctx, asset.ID, "ASSET_RUN_BUDGET_EXCEEDED")
	}
	data, err := service.provider.FetchAsset(ctx, asset.Reference)
	if err != nil {
		return consumed, service.fail(ctx, asset.ID, stableAssetError(err))
	}
	consumed += int64(len(data.Bytes))
	if consumed > 100<<20 {
		return consumed, service.fail(ctx, asset.ID, "ASSET_RUN_BUDGET_EXCEEDED")
	}
	blob, err := service.blobs.Put(bytes.NewReader(data.Bytes))
	if err != nil {
		return consumed, fmt.Errorf("store metadata asset bytes: %w", err)
	}
	err = service.records.Publish(ctx, AssetPublication{
		ID: asset.ID, Blob: blob, MediaType: data.MediaType,
		Width: data.Width, Height: data.Height, Now: service.now().UnixMilli(),
	})
	if err != nil {
		return consumed, fmt.Errorf("publish metadata asset: %w", err)
	}
	return consumed, nil
}

func (service *AssetService) fail(ctx context.Context, id, code string) error {
	if err := service.records.Fail(ctx, id, code, service.now().UnixMilli()); err != nil {
		return fmt.Errorf("mark metadata asset failed: %w", err)
	}
	return nil
}
