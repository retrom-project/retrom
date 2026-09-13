package metadatascrape

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
)

type memoryCache struct {
	value CachedResponse
	found bool
	err   error
	calls int
	now   int64
}

func (cache *memoryCache) Cached(_ context.Context, _ string, now int64) (CachedResponse, bool, error) {
	cache.calls++
	cache.now = now
	return cache.value, cache.found, cache.err
}

type lookupProvider struct {
	network, restored int
	restoreErr        error
}

func (provider *lookupProvider) LookupByHash(context.Context, hasheous.ContentHashes) (hasheous.LookupResult, error) {
	provider.network++
	return hasheous.LookupResult{Outcome: hasheous.OutcomeMiss}, nil
}

func (provider *lookupProvider) RestoreCached(_ hasheous.ContentHashes, outcome hasheous.ProviderOutcome, _ int, _ []byte) (hasheous.LookupResult, error) {
	provider.restored++
	return hasheous.LookupResult{Outcome: outcome}, provider.restoreErr
}

type cacheScenario struct {
	name                        string
	found, bypass, raw, corrupt bool
	outcome                     hasheous.ProviderOutcome
	network, restored           int
}

func TestMetadataCacheUsesValidResponseOrFallsBackToProvider(t *testing.T) {
	for _, test := range []cacheScenario{
		{name: "absent", network: 1},
		{name: "bypass", found: true, bypass: true, network: 1},
		{name: "cached miss", found: true, outcome: hasheous.OutcomeMiss, restored: 1},
		{name: "cached hit", found: true, raw: true, outcome: hasheous.OutcomeHit, restored: 1},
		{name: "missing raw", found: true, outcome: hasheous.OutcomeHit, network: 1},
		{name: "corrupt response", found: true, raw: true, corrupt: true, outcome: hasheous.OutcomeHit, network: 1, restored: 1},
	} {
		t.Run(test.name, func(t *testing.T) { assertCacheScenario(t, test) })
	}
}

func TestMetadataCacheStorageFailureDoesNotBecomeMiss(t *testing.T) {
	provider := &lookupProvider{}
	service := NewLookup(&memoryCache{err: context.DeadlineExceeded}, nil, provider, time.Now)
	_, err := service.Lookup(t.Context(), hasheous.ContentHashes{SHA256: strings.Repeat("a", 64)}, false)
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
	cache := &memoryCache{found: test.found, value: CachedResponse{ID: "response", Outcome: test.outcome}}
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
	result, err := service.Lookup(t.Context(), hasheous.ContentHashes{SHA256: strings.Repeat("a", 64)}, test.bypass)
	if err != nil {
		t.Fatal(err)
	}
	assertCacheOutcome(t, test, provider, cache, result)
}

func assertCacheOutcome(t *testing.T, test cacheScenario, provider *lookupProvider, cache *memoryCache, result ResolvedLookup) {
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
