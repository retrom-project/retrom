package metadatascrape

import (
	"context"
	"fmt"
	"io"
	"time"

	model "retrom/internal/model/metadatascrape"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/foundation/cleanup"
)

type LookupProvider interface {
	LookupByHash(context.Context, hasheous.ContentHashes) (hasheous.LookupResult, error)
	RestoreCached(hasheous.ContentHashes, hasheous.ProviderOutcome, int, []byte) (hasheous.LookupResult, error)
}
type LookupService struct {
	records  model.CacheReader
	blobs    *blobstore.Store
	provider LookupProvider
	now      func() time.Time
}

func NewLookup(
	records model.CacheReader,
	blobs *blobstore.Store,
	provider LookupProvider,
	now func() time.Time,
) *LookupService {
	return &LookupService{records: records, blobs: blobs, provider: provider, now: now}
}

func (service *LookupService) Lookup(
	ctx context.Context,
	hashes hasheous.ContentHashes,
	bypassCache bool,
) (model.ResolvedLookup, error) {
	digest, err := hasheous.RequestDigest(hashes)
	if err != nil {
		return model.ResolvedLookup{}, fmt.Errorf("digest metadata request: %w", err)
	}
	if !bypassCache {
		cached, found, err := service.cached(ctx, digest, hashes)
		if err != nil {
			return model.ResolvedLookup{}, err
		}
		if found {
			return cached, nil
		}
	}
	result, err := service.provider.LookupByHash(ctx, hashes)
	if err != nil {
		return model.ResolvedLookup{}, fmt.Errorf("look up metadata by hash: %w", err)
	}
	return model.ResolvedLookup{Result: result}, nil
}

func (service *LookupService) cached(
	ctx context.Context,
	digest string,
	hashes hasheous.ContentHashes,
) (model.ResolvedLookup, bool, error) {
	entry, found, err := service.records.Cached(ctx, digest, service.now().UnixMilli())
	if err != nil {
		return model.ResolvedLookup{}, false, fmt.Errorf("read metadata cache: %w", err)
	}
	if !found {
		return model.ResolvedLookup{}, false, nil
	}
	raw := service.readCachedResponse(entry.RawSHA256)
	if entry.Outcome != hasheous.OutcomeMiss && len(raw) == 0 {
		return model.ResolvedLookup{}, false, nil
	}
	result, err := service.provider.RestoreCached(hashes, entry.Outcome, entry.HTTPStatus, raw)
	if err == nil {
		return model.ResolvedLookup{Result: result, CachedResponseID: entry.ID}, true, nil
	}
	return model.ResolvedLookup{}, false, nil
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

func ResponseExpiry(outcome hasheous.ProviderOutcome, now int64) int64 {
	switch outcome {
	case hasheous.OutcomeHit:
		return now + int64(7*24*time.Hour/time.Millisecond)
	case hasheous.OutcomeMiss:
		return now + int64(24*time.Hour/time.Millisecond)
	case hasheous.OutcomeRateLimited, hasheous.OutcomeTimeout, hasheous.OutcomeInvalidResponse,
		hasheous.OutcomeNetworkError:
		return now
	}
	return now
}
