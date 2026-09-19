package metadatascrape

import (
	"context"

	metadatamodel "retrom/internal/model/metadata"
)

type CachedResponse struct {
	ID, RawSHA256 string
	Outcome       metadatamodel.ProviderOutcome
	Audit         metadatamodel.ProtocolAudit
}

type CacheReader interface {
	Cached(context.Context, string, int64) (CachedResponse, bool, error)
}
