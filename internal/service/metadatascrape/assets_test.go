package metadatascrape

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/hasheous"
)

type assetMemory struct {
	pending     []PendingAsset
	failedCode  string
	failure     error
	publication AssetPublication
	publishes   int
}

func (records *assetMemory) Pending(context.Context, string) ([]PendingAsset, error) {
	return records.pending, nil
}

func (records *assetMemory) Publish(_ context.Context, value AssetPublication) error {
	records.publication = value
	records.publishes++
	return records.failure
}

func (records *assetMemory) Fail(_ context.Context, _, code string, _ int64) error {
	records.failedCode = code
	return records.failure
}

type assetFetcher struct {
	data  hasheous.AssetData
	err   error
	calls int
}

func (provider *assetFetcher) FetchAsset(context.Context, hasheous.AssetRef) (hasheous.AssetData, error) {
	provider.calls++
	return provider.data, provider.err
}

type assetBytes struct {
	calls int
	bytes string
	err   error
}

func (blobs *assetBytes) Put(reader io.Reader) (blobstore.Metadata, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return blobstore.Metadata{}, err
	}
	blobs.calls++
	blobs.bytes = string(data)
	return blobstore.Metadata{SHA256: "digest", Size: int64(len(data))}, blobs.err
}

func TestAssetBudgetPreventsDownloadOrPublication(t *testing.T) {
	for _, test := range []struct {
		name     string
		consumed int64
		calls    int
	}{
		{"already exhausted", 100 << 20, 0}, {"crosses budget", 100<<20 - 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			records, provider, blobs := &assetMemory{}, &assetFetcher{data: hasheous.AssetData{Bytes: []byte("ab")}}, &assetBytes{}
			service := NewAssets(records, provider, blobs, func() time.Time { return time.UnixMilli(10) })
			_, err := service.fetch(t.Context(), PendingAsset{ID: "asset"}, test.consumed)
			if err != nil {
				t.Fatal(err)
			}
			if provider.calls != test.calls || blobs.calls != 0 || records.publishes != 0 || records.failedCode != "ASSET_RUN_BUDGET_EXCEEDED" {
				t.Fatalf("budget policy: calls=%d writes=%d publication=%d error=%s", provider.calls, blobs.calls, records.publishes, records.failedCode)
			}
		})
	}
}

func TestAssetPublicationUsesPreparedBytesAndOneTimestamp(t *testing.T) {
	records := &assetMemory{pending: []PendingAsset{{ID: "asset"}}}
	provider := &assetFetcher{data: hasheous.AssetData{Bytes: []byte("image"), MediaType: "image/png", Width: 4, Height: 3}}
	blobs := &assetBytes{}
	clocks := 0
	service := NewAssets(records, provider, blobs, func() time.Time { clocks++; return time.UnixMilli(int64(clocks * 10)) })
	if err := service.Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	value := records.publication
	if blobs.bytes != "image" || clocks != 1 || value.ID != "asset" || value.Now != 10 || value.Width != 4 || value.Height != 3 || value.MediaType != "image/png" || value.Blob.SHA256 != "digest" {
		t.Fatalf("publication: %+v clocks=%d bytes=%s", value, clocks, blobs.bytes)
	}
}

func TestAssetErrorsRemainStableAndPersistenceFailuresPropagate(t *testing.T) {
	records := &assetMemory{failure: context.DeadlineExceeded}
	provider := &assetFetcher{err: hasheous.ErrAssetIPRejected}
	service := NewAssets(records, provider, &assetBytes{}, func() time.Time { return time.UnixMilli(1) })
	_, err := service.fetch(t.Context(), PendingAsset{ID: "asset"}, 0)
	if !errors.Is(err, context.DeadlineExceeded) || records.failedCode != "ASSET_IP_REJECTED" {
		t.Fatalf("failure: %s / %v", records.failedCode, err)
	}
}

func TestCancelledAssetFetchDoesNotDownload(t *testing.T) {
	records, provider := &assetMemory{}, &assetFetcher{}
	service := NewAssets(records, provider, &assetBytes{}, time.Now)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := service.fetch(ctx, PendingAsset{}, 0)
	if !errors.Is(err, context.Canceled) || provider.calls != 0 || records.publishes != 0 {
		t.Fatalf("cancelled fetch: calls=%d error=%v", provider.calls, err)
	}
}
