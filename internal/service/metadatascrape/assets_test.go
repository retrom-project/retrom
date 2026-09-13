package metadatascrape

import (
	"context"
	"errors"
	"io"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
)

type assetFetcher struct {
	data  hasheous.AssetData
	err   error
	calls int
	limit int64
}

func (provider *assetFetcher) FetchAssetBounded(_ context.Context, _ hasheous.AssetRef, limit int64) (hasheous.AssetData, error) {
	provider.calls++
	provider.limit = limit
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
		{"already exhausted", MediaRunBudget, 0}, {"crosses budget", MediaRunBudget - 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory, err := newMediaMemory()
			if err != nil {
				t.Fatal(err)
			}
			memory.snapshot.Charged = test.consumed
			provider := &assetFetcher{data: hasheous.AssetData{ReceivedBytes: 1}, err: hasheous.ErrAssetReadLimit}
			blobs := &assetBytes{}
			err = NewMediaWorker(memory, provider, blobs, mediaUnitNow).Run(t.Context(), memory.snapshot.Job.ID)
			if !errors.Is(err, hasheous.ErrAssetReadLimit) {
				t.Fatalf("budget cause=%v", err)
			}
			if provider.calls != test.calls || blobs.calls != 0 || memory.publication.ID != "" || memory.outcome.Code != "ASSET_RUN_BUDGET_EXCEEDED" {
				t.Fatalf("budget calls=%d publication=%+v outcome=%+v", provider.calls, memory.publication, memory.outcome)
			}
		})
	}
}

func TestAssetPublicationUsesPreparedBytesAndOneTimestamp(t *testing.T) {
	memory, err := newMediaMemory()
	if err != nil {
		t.Fatal(err)
	}
	provider := &assetFetcher{data: hasheous.AssetData{Bytes: []byte("image"), ReceivedBytes: 5, MediaType: "image/png", Width: 4, Height: 3}}
	blobs := &assetBytes{}
	if err := NewMediaWorker(memory, provider, blobs, mediaUnitNow).Run(t.Context(), memory.snapshot.Job.ID); err != nil {
		t.Fatal(err)
	}
	value := memory.publication
	if blobs.bytes != "image" || value.ID != "asset" || value.Now != 100 || memory.outcome.Now != value.Now ||
		value.Width != 4 || value.Height != 3 || value.MediaType != "image/png" || value.Blob.SHA256 != "digest" {
		t.Fatalf("publication=%+v outcome=%+v bytes=%s", value, memory.outcome, blobs.bytes)
	}
}

func TestAssetErrorsRemainStableAndPersistenceFailuresPropagate(t *testing.T) {
	memory, err := newMediaMemory()
	if err != nil {
		t.Fatal(err)
	}
	memory.failure = context.DeadlineExceeded
	provider := &assetFetcher{err: hasheous.ErrAssetIPRejected}
	err = NewMediaWorker(memory, provider, &assetBytes{}, mediaUnitNow).Run(t.Context(), memory.snapshot.Job.ID)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, hasheous.ErrAssetIPRejected) {
		t.Fatalf("failure=%v", err)
	}
}

func TestCancelledAssetFetchDoesNotDownload(t *testing.T) {
	memory, err := newMediaMemory()
	if err != nil {
		t.Fatal(err)
	}
	provider := &assetFetcher{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = NewMediaWorker(memory, provider, &assetBytes{}, mediaUnitNow).Run(ctx, memory.snapshot.Job.ID)
	if !errors.Is(err, context.Canceled) || provider.calls != 0 || memory.publication.ID != "" {
		t.Fatalf("cancel=%v calls=%d", err, provider.calls)
	}
}

func TestMediaCASFailureKeepsReceivedBytesCharged(t *testing.T) {
	memory, err := newMediaMemory()
	if err != nil {
		t.Fatal(err)
	}
	provider := &assetFetcher{data: hasheous.AssetData{Bytes: []byte("image"), ReceivedBytes: 5}}
	cause := errors.New("CAS unavailable")
	err = NewMediaWorker(memory, provider, &assetBytes{err: cause}, mediaUnitNow).Run(t.Context(), memory.snapshot.Job.ID)
	if !errors.Is(err, cause) || memory.snapshot.Charged != 5 || memory.snapshot.Asset.Reserved != 0 || memory.outcome.State != "FAILED" {
		t.Fatalf("CAS failure reset budget: charged=%d outcome=%+v cause=%v", memory.snapshot.Charged, memory.outcome, err)
	}
}

func TestMediaCancellationBeforeSourceRefundsUnreadReservation(t *testing.T) {
	memory, err := newMediaMemory()
	if err != nil {
		t.Fatal(err)
	}
	source := &assetFetcher{}
	worker := NewMediaWorker(memory, source, &assetBytes{}, mediaUnitNow)
	execution, err := worker.claim(t.Context(), memory.snapshot.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, cause := worker.fetch(ctx, execution)
	if !errors.Is(cause, context.Canceled) || source.calls != 0 || memory.snapshot.Charged != 0 || memory.snapshot.Asset.Reserved != 0 {
		t.Fatalf("unread cancellation charged bytes: calls=%d charged=%d reserved=%d cause=%v", source.calls,
			memory.snapshot.Charged, memory.snapshot.Asset.Reserved, cause)
	}
}
