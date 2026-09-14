package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/metadatascrape"
)

type CacheRepository struct{ database *sql.DB }

func NewCache(database *sql.DB) *CacheRepository { return &CacheRepository{database: database} }
func (repository *CacheRepository) Cached(
	ctx context.Context,
	digest string,
	now int64,
) (metadatascrape.CachedResponse, bool, error) {
	var entry metadatascrape.CachedResponse
	var status sql.NullInt64
	var raw sql.NullString
	err := repository.database.QueryRowContext(ctx, `SELECT r.id,r.outcome,r.http_status,b.sha256
 FROM metadata_provider_cache c JOIN metadata_provider_responses r ON r.id=c.current_response_id
 LEFT JOIN blobs b ON b.id=r.raw_response_blob_id
 WHERE c.provider='HASHEOUS' AND c.request_digest=? AND c.expires_at_ms>?`, digest, now).
		Scan(&entry.ID, &entry.Outcome, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return metadatascrape.CachedResponse{}, false, nil
	}
	if err != nil {
		return metadatascrape.CachedResponse{}, false, fmt.Errorf("query metadata cache: %w", err)
	}
	entry.HTTPStatus = int(status.Int64)
	entry.RawSHA256 = raw.String
	return entry, true, nil
}
