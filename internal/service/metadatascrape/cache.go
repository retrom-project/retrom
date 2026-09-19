package metadatascrape

import (
	"context"
	"fmt"
	"io"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/foundation/cleanup"
	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type LookupService struct {
	records  metadatascrapemodel.CacheReader
	blobs    *blobstore.Store
	provider metadatascrapemodel.LookupProvider
	now      func() time.Time
}

func NewLookup(
	records metadatascrapemodel.CacheReader, blobs *blobstore.Store,
	provider metadatascrapemodel.LookupProvider, now func() time.Time,
) *LookupService {
	return &LookupService{records: records, blobs: blobs, provider: provider, now: now}
}

func (service *LookupService) Lookup(
	ctx context.Context,
	hashes metadatamodel.ContentHashes,
	bypassCache bool,
) (metadatascrapemodel.ResolvedLookup, error) {
	digest, err := metadatamodel.RequestDigest(hashes)
	if err != nil {
		return metadatascrapemodel.ResolvedLookup{}, fmt.Errorf("digest metadata request: %w", err)
	}
	if !bypassCache {
		cached, found, err := service.cached(ctx, digest, hashes)
		if err != nil {
			return metadatascrapemodel.ResolvedLookup{}, err
		}
		if found {
			return cached, nil
		}
	}
	result, err := service.provider.LookupByHash(ctx, hashes)
	if err != nil {
		return metadatascrapemodel.ResolvedLookup{}, fmt.Errorf("look up metadata by hash: %w", err)
	}
	return metadatascrapemodel.ResolvedLookup{Result: result}, nil
}

func (service *LookupService) cached(
	ctx context.Context,
	digest string,
	hashes metadatamodel.ContentHashes,
) (metadatascrapemodel.ResolvedLookup, bool, error) {
	entry, found, err := service.records.Cached(ctx, digest, service.now().UnixMilli())
	if err != nil {
		return metadatascrapemodel.ResolvedLookup{}, false, fmt.Errorf("read metadata cache: %w", err)
	}
	if !found {
		return metadatascrapemodel.ResolvedLookup{}, false, nil
	}
	raw := service.readCachedResponse(entry.RawSHA256)
	if entry.Outcome != metadatamodel.OutcomeMiss && len(raw) == 0 {
		return metadatascrapemodel.ResolvedLookup{}, false, nil
	}
	result, err := service.provider.RestoreCached(hashes, entry.Outcome, entry.Audit, raw)
	if err == nil {
		return metadatascrapemodel.ResolvedLookup{Result: result, CachedResponseID: entry.ID}, true, nil
	}
	return metadatascrapemodel.ResolvedLookup{}, false, nil
}

func (service *LookupService) readCachedResponse(digest string) []byte {
	if digest == "" {
		return nil
	}
	file, err := service.blobs.OpenDigest(digest)
	if err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	cleanup.Error("close metadata cache response", file.Close())
	if err != nil || len(raw) > 4<<20 {
		return nil
	}
	return raw
}

func ResponseExpiry(outcome metadatamodel.ProviderOutcome, now int64) int64 {
	switch outcome {
	case metadatamodel.OutcomeHit:
		return now + int64(7*24*time.Hour/time.Millisecond)
	case metadatamodel.OutcomeMiss:
		return now + int64(24*time.Hour/time.Millisecond)
	case metadatamodel.OutcomeRateLimited, metadatamodel.OutcomeTimeout, metadatamodel.OutcomeInvalidResponse,
		metadatamodel.OutcomeNetworkError:
		return now
	}
	return now
}
