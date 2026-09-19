package metadatascrape

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type memoryCache struct {
	value metadatascrapemodel.CachedResponse
	found bool
	err   error
	calls int
	now   int64
}

func (cache *memoryCache) Cached(_ context.Context, _ string, now int64) (metadatascrapemodel.CachedResponse, bool, error) {
	cache.calls++
	cache.now = now
	return cache.value, cache.found, cache.err
}

type lookupProvider struct {
	network, restored int
	restoreErr        error
}

func (provider *lookupProvider) LookupByHash(context.Context, metadatamodel.ContentHashes) (metadatamodel.LookupResult, error) {
	provider.network++
	return metadatamodel.LookupResult{Outcome: metadatamodel.OutcomeMiss}, nil
}

func (provider *lookupProvider) RestoreCached(_ metadatamodel.ContentHashes, outcome metadatamodel.ProviderOutcome, _ metadatamodel.ProtocolAudit, _ []byte) (metadatamodel.LookupResult, error) {
	provider.restored++
	return metadatamodel.LookupResult{Outcome: outcome}, provider.restoreErr
}

type cacheScenario struct {
	name                        string
	found, bypass, raw, corrupt bool
	outcome                     metadatamodel.ProviderOutcome
	network, restored           int
}

func TestMetadataCacheUsesValidResponseOrFallsBackToProvider(t *testing.T) {
	for _, test := range []cacheScenario{
		{name: "absent", network: 1},
		{name: "bypass", found: true, bypass: true, network: 1},
		{name: "cached miss", found: true, outcome: metadatamodel.OutcomeMiss, restored: 1},
		{name: "cached hit", found: true, raw: true, outcome: metadatamodel.OutcomeHit, restored: 1},
		{name: "missing raw", found: true, outcome: metadatamodel.OutcomeHit, network: 1},
		{name: "corrupt response", found: true, raw: true, corrupt: true, outcome: metadatamodel.OutcomeHit, network: 1, restored: 1},
	} {
		t.Run(test.name, func(t *testing.T) { assertCacheScenario(t, test) })
	}
}

func TestMetadataCacheStorageFailureDoesNotBecomeMiss(t *testing.T) {
	provider := &lookupProvider{}
	service := NewLookup(&memoryCache{err: context.DeadlineExceeded}, nil, provider, time.Now)
	_, err := service.Lookup(t.Context(), metadatamodel.ContentHashes{SHA256: strings.Repeat("a", 64)}, false)
	if !errors.Is(err, context.DeadlineExceeded) || provider.network != 0 {
		t.Fatalf("cache error=%v network=%d", err, provider.network)
	}
}

func assertCacheScenario(t *testing.T, test cacheScenario) {
	t.Helper()

	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache := &memoryCache{found: test.found, value: metadatascrapemodel.CachedResponse{ID: "response", Outcome: test.outcome}}
	if test.raw {
		metadata, err := blobs.Put(strings.NewReader(`{"data":"cached"}`))
		if err != nil {
			t.Fatal(err)
		}
		cache.value.RawSHA256 = metadata.SHA256
	}
	provider := &lookupProvider{}
	if test.corrupt {
		provider.restoreErr = context.Canceled
	}
	service := NewLookup(cache, blobs, provider, func() time.Time { return time.UnixMilli(100) })
	result, err := service.Lookup(t.Context(), metadatamodel.ContentHashes{SHA256: strings.Repeat("a", 64)}, test.bypass)
	if err != nil {
		t.Fatal(err)
	}
	assertCacheOutcome(t, test, provider, cache, result)
}

func assertCacheOutcome(t *testing.T, test cacheScenario, provider *lookupProvider, cache *memoryCache, result metadatascrapemodel.ResolvedLookup) {
	t.Helper()
	if provider.network != test.network || provider.restored != test.restored {
		t.Fatalf("network=%d restore=%d", provider.network, provider.restored)
	}
	if test.bypass && cache.calls != 0 {
		t.Fatal("forced lookup consulted cache")
	}
	if !test.bypass && cache.now != 100 {
		t.Fatalf("cache time=%d", cache.now)
	}
	if test.network == 0 && result.CachedResponseID != "response" {
		t.Fatal("cache evidence lost")
	}
	if test.network != 0 && result.CachedResponseID != "" {
		t.Fatal("network response attributed to cache")
	}
}
