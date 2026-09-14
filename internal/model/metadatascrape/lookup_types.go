package metadatascrape

import "retrom/internal/adapter/metadata/hasheous"

type ResolvedLookup struct {
	Result           hasheous.LookupResult
	CachedResponseID string
}
