package recordstore

import (
	"context"
	"database/sql"
)

func CreateMetadataProviderCache(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "provider,request_digest", ValidateMetadataProviderCache)
}

func ValidateMetadataProviderCache(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, metadata_provider_cacheOwnership, keys)
}

const metadata_provider_cacheOwnership = `
SELECT CASE
WHEN ((NOT EXISTS (
    SELECT 1 FROM metadata_provider_responses
    WHERE id = candidate.current_response_id AND provider = candidate.provider AND request_digest =
candidate.request_digest
  ))) THEN 'provider cache response mismatch'
ELSE '' END
FROM metadata_provider_cache candidate
WHERE candidate.provider=? AND candidate.request_digest=?`
