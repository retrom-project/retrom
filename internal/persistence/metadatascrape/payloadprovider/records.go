package payloadprovider

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

type Records struct{ Executor dbapi.Executor }

const providerNoRunning = `NOT EXISTS(
SELECT 1 FROM metadata_scrape_query_attempts attempt
JOIN metadata_scrape_runs run ON run.id=attempt.scrape_run_id
WHERE attempt.provider_response_id=metadata_provider_responses.id AND run.state='RUNNING')`

func (records Records) Providers(
	ctx context.Context, now int64, limit int,
) ([]application.ProviderExpiration, error) {
	rows, err := records.Executor.QueryContext(ctx, `SELECT id,raw_response_blob_id,raw_payload_state,expires_at_ms,
(SELECT count(*) FROM metadata_provider_cache WHERE current_response_id=metadata_provider_responses.id)
FROM metadata_provider_responses WHERE raw_payload_state='RETAINED' AND expires_at_ms<=?
AND `+providerNoRunning+` ORDER BY expires_at_ms,id LIMIT ?`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("select expired provider payloads: %w", err)
	}
	defer func() { cleanup.Error("close expired provider payloads", rows.Close()) }()
	var facts []application.ProviderExpiration
	for rows.Next() {
		var row application.ProviderExpiration
		if err := rows.Scan(&row.ID, &row.BlobID, &row.State, &row.ExpiresMS, &row.CacheCount); err != nil {
			return nil, fmt.Errorf("scan expired provider payload: %w", err)
		}
		facts = append(facts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired provider payloads: %w", err)
	}
	return facts, nil
}

func (records Records) ReleaseProvider(
	ctx context.Context, before application.ProviderExpiration, now int64,
) error {
	result, err := records.Executor.ExecContext(ctx, `DELETE FROM metadata_provider_cache WHERE current_response_id=?`,
		before.ID)
	if err := expirationWrite(result, err, before.CacheCount); err != nil {
		return fmt.Errorf("release expired provider cache: %w", err)
	}
	result, err = recordstore.UpdateMetadataProviderResponses(ctx, records.Executor, recordstore.Update{
		Set: `raw_response_blob_id=NULL,raw_payload_state='RELEASED',raw_payload_released_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND raw_payload_state=? AND raw_response_blob_id=? AND expires_at_ms=? AND expires_at_ms<=?
AND ` + providerNoRunning,
			Args: []any{before.ID, before.State, before.BlobID, before.ExpiresMS, now},
		},
		Values: []any{now},
	})
	if err := expirationWrite(result, err, 1); err != nil {
		return fmt.Errorf("release expired provider response: %w", err)
	}
	return nil
}

func expirationWrite(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write expiration record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count expiration writes: %w", err)
	}
	if count != expected {
		return application.ErrExpirationSnapshotChanged
	}
	return nil
}
