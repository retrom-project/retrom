package metadatascrape

import (
	"context"

	metadatamodel "retrom/internal/model/metadata"
)

type ResolvedLookup struct {
	Result           metadatamodel.LookupResult
	CachedResponseID string
}

// LookupProvider supplies normalized metadata and revalidates immutable cached evidence.
type LookupProvider interface {
	LookupByHash(context.Context, metadatamodel.ContentHashes) (metadatamodel.LookupResult, error)
	RestoreCached(
		metadatamodel.ContentHashes, metadatamodel.ProviderOutcome, metadatamodel.ProtocolAudit, []byte,
	) (metadatamodel.LookupResult, error)
}
