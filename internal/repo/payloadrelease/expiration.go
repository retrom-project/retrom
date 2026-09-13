package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/payloadrelease"
)

type Expiration struct{ database *sql.DB }

func NewExpiration(database *sql.DB) *Expiration { return &Expiration{database: database} }

func (repository *Expiration) WithExpiration(ctx context.Context, run func(application.ExpirationScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payload expiration: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := expirationRecords{executor: tx}
	if err := run(application.ExpirationScope{Read: records, Write: records, GC: BindGC(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payload expiration: %w", err)
	}
	return nil
}

type expirationRecords struct{ executor dbexec.Executor }

const providerNoRunning = `NOT EXISTS(
SELECT 1 FROM metadata_scrape_query_attempts attempt
JOIN metadata_scrape_runs run ON run.id=attempt.scrape_run_id
WHERE attempt.provider_response_id=metadata_provider_responses.id AND run.state='RUNNING')`

func (records expirationRecords) Providers(
	ctx context.Context, now int64, limit int,
) ([]application.ProviderExpiration, error) {
	rows, err := records.executor.QueryContext(ctx, `SELECT id,raw_response_blob_id,raw_payload_state,expires_at_ms,
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

const previewExpiryDue = `(state='CREATED' AND bootstrap_expires_at_ms<=? OR hard_expires_at_ms<=? OR state='REVOKED')
AND (state NOT IN ('EXPIRED','REVOKED') OR checkpoint_payload_blob_id IS NOT NULL
OR restore_payload_blob_id IS NOT NULL)`

func (records expirationRecords) Previews(
	ctx context.Context, now int64, limit int,
) ([]application.PreviewExpiration, error) {
	rows, err := records.executor.QueryContext(ctx, `SELECT id,state,version,bootstrap_expires_at_ms,hard_expires_at_ms,
finished_at_ms,COALESCE(checkpoint_payload_blob_id,''),COALESCE(restore_payload_blob_id,'')
FROM review_preview_sessions WHERE `+previewExpiryDue+` ORDER BY hard_expires_at_ms,id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, fmt.Errorf("select expired previews: %w", err)
	}
	defer func() { cleanup.Error("close expired previews", rows.Close()) }()
	var facts []application.PreviewExpiration
	for rows.Next() {
		var row application.PreviewExpiration
		if err := rows.Scan(&row.ID, &row.State, &row.Version, &row.BootstrapExpiresMS, &row.HardExpiresMS,
			&row.FinishedMS, &row.CheckpointBlobID, &row.RestoreBlobID); err != nil {
			return nil, fmt.Errorf("scan expired preview: %w", err)
		}
		facts = append(facts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired previews: %w", err)
	}
	return facts, nil
}
