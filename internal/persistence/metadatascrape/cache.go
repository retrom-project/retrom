package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/service/metadatascrape"
)

type CacheRepository struct{ database dbapi.DB }

func NewCache(database dbapi.DB) *CacheRepository { return &CacheRepository{database: database} }
func (repository *CacheRepository) Cached(
	ctx context.Context,
	digest string,
	now int64,
) (metadatascrape.CachedResponse, bool, error) {
	var entry metadatascrape.CachedResponse
	var status sql.NullInt64
	var raw sql.NullString
	err := dbapi.QueryRowContext(ctx, repository.database, `SELECT r.id,r.outcome,r.http_status,b.id
 FROM metadata_provider_cache c JOIN metadata_provider_responses r ON r.id=c.current_response_id
 LEFT JOIN stored_files b ON b.id=r.raw_response_blob_id
 WHERE c.provider='HASHEOUS' AND c.request_digest=? AND c.expires_at_ms>?`, digest, now).
		Scan(&entry.ID, &entry.Outcome, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return metadatascrape.CachedResponse{}, false, nil
	}
	if err != nil {
		return metadatascrape.CachedResponse{}, false, fmt.Errorf("query metadata cache: %w", err)
	}
	entry.HTTPStatus = int(status.Int64)
	entry.RawFileID = raw.String
	return entry, true, nil
}
