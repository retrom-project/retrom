package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type (
	ResultRepository struct{ database *sql.DB }
	resultRecords    struct{ transaction *sql.Tx }
)

func NewRecorder(database *sql.DB) *ResultRepository { return &ResultRepository{database: database} }

func (repository *ResultRepository) WithWrite(ctx context.Context, work func(metadatascrape.ResultScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scrape result: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := resultRecords{transaction}
	if err := work(metadatascrape.ResultScope{Read: records, Write: records, Media: records}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit scrape result records: %w", err)
	}
	return nil
}

func (records resultRecords) Writable(ctx context.Context, claim metadatascrape.WorkerClaim) (bool, error) {
	var allowed bool
	err := records.transaction.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM metadata_scrape_runs r
 JOIN jobs j ON j.id=r.job_id LEFT JOIN games g ON g.id=r.game_id WHERE r.id=? AND j.id=? AND j.execution_no=?
 AND j.worker_id=? AND j.state='RUNNING' AND r.state='RUNNING' AND j.leased_until_ms>? AND j.execution_deadline_at_ms>?
 AND (r.game_id IS NULL OR g.status='PUBLISHED'))`,
		claim.RunID,
		claim.JobID,
		claim.ExecutionNo,
		claim.WorkerID,
		claim.Now,
		claim.Now,
	).Scan(
		&allowed,
	)
	if err != nil {
		return false, fmt.Errorf("query result execution ownership: %w", err)
	}
	return allowed, nil
}

func (records resultRecords) Hashes(ctx context.Context, id string) (metadatascrape.Hashes, error) {
	var hashes metadatascrape.Hashes
	err := records.transaction.QueryRowContext(
		ctx,
		`SELECT crc32,md5,sha1,sha256 FROM content_hash_evidence WHERE id=?`,
		id,
	).
		Scan(&hashes.CRC32, &hashes.MD5, &hashes.SHA1, &hashes.SHA256)
	if err != nil {
		return hashes, fmt.Errorf("query candidate evidence hashes: %w", err)
	}
	return hashes, nil
}

func (records resultRecords) Response(ctx context.Context, value metadatascrape.ResponseRecord) error {
	var blobID *string
	state := "NONE"
	if value.Blob != nil {
		id, err := blobcatalog.EnsureRecord(ctx, records.transaction, *value.Blob, "application/json", value.Now)
		if err != nil {
			return fmt.Errorf("register raw provider response: %w", err)
		}
		blobID = &id
		state = "RETAINED"
	}
	var status *int
	if value.HTTPStatus != 0 {
		status = &value.HTTPStatus
	}
	_, err := records.transaction.ExecContext(
		ctx,
		`INSERT INTO metadata_provider_responses
 (id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
 VALUES(?,'HASHEOUS',?,?,?,?,?,?,?)`,
		value.ID,
		value.RequestDigest,
		status,
		value.Outcome,
		blobID,
		state,
		value.Now,
		value.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert provider response: %w", err)
	}
	if !value.Cacheable {
		return nil
	}
	_, err = recordstore.CreateMetadataProviderCache(
		ctx,
		records.transaction,
		`INSERT INTO metadata_provider_cache
 (provider,request_digest,current_response_id,expires_at_ms,updated_at_ms) VALUES('HASHEOUS',?,?,?,?)
 ON CONFLICT(provider,request_digest) DO UPDATE SET current_response_id=excluded.current_response_id,
 expires_at_ms=excluded.expires_at_ms,updated_at_ms=excluded.updated_at_ms`,
		value.RequestDigest,
		value.ID,
		value.ExpiresAt,
		value.Now,
	)
	if err != nil {
		return fmt.Errorf("upsert provider response cache: %w", err)
	}
	return nil
}

func (records resultRecords) Attempt(ctx context.Context, value metadatascrape.AttemptRecord) error {
	_, err := records.transaction.ExecContext(ctx, `INSERT INTO metadata_scrape_query_attempts
 (id,scrape_run_id,content_hash_evidence_id,provider_response_id,attempt_no,source,created_at_ms)
 VALUES(?,?,?,?,?,?,?)`,
		value.ID, value.RunID, value.EvidenceID, value.ResponseID, value.AttemptNo, value.Source, value.Now)
	if err != nil {
		return fmt.Errorf("insert scrape attempt: %w", err)
	}
	return nil
}
