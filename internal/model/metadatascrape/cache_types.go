package metadatascrape

import (
	"context"

	"retrom/internal/adapter/metadata/hasheous"
)

type CachedResponse struct {
	ID, RawSHA256 string
	Outcome       hasheous.ProviderOutcome
	HTTPStatus    int
}

type CacheReader interface {
	Cached(context.Context, string, int64) (CachedResponse, bool, error)
}
