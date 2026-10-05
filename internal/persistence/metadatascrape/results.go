package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/metadatascrape"
)

type (
	ResultRepository struct{ database dbapi.DB }
	resultRecords    struct{ transaction dbapi.Tx }
)

func NewRecorder(database dbapi.DB) *ResultRepository { return &ResultRepository{database: database} }

func (repository *ResultRepository) WithWrite(
	ctx context.Context,
	work func(metadatascrape.ResultScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		records := resultRecords{transaction}
		if err := work(metadatascrape.ResultScope{Read: records, Write: records, Media: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit scrape result records: %w", err)
	}
	return nil
}

func (records resultRecords) Writable(ctx context.Context, claim metadatascrape.WorkerClaim) (bool, error) {
	var allowed bool
	err := dbapi.QueryRowContext(
		ctx, records.transaction,
		`SELECT EXISTS(SELECT 1 FROM metadata_scrape_runs r
 JOIN jobs j ON j.id=r.job_id LEFT JOIN games g ON g.id=r.game_id WHERE r.id=? AND j.id=?
AND
j.execution_no=?
 AND j.worker_id=? AND j.state='RUNNING' AND r.state='RUNNING' AND j.leased_until_ms>?
AND
j.execution_deadline_at_ms>?
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
	err := dbapi.QueryRowContext(
		ctx, records.transaction,
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
	var fileRecord *string
	state := "NONE"
	if value.Blob != nil {
		id, err := filestore.FileRecord(*value.Blob, "application/json")
		if err != nil {
			return fmt.Errorf("register raw provider response: %w", err)
		}

		fileRecord = &id
		state = "RETAINED"
	}
	var status *int
	if value.HTTPStatus != 0 {
		status = &value.HTTPStatus
	}
	_, err := recordstore.InsertRows(
		ctx,
		records.transaction,
		"metadata_provider_responses",
		`INSERT INTO metadata_provider_responses
 (id,provider,request_digest,http_status,outcome,raw_response_file_record,raw_payload_state,
fetched_at_ms,expires_at_ms)
 VALUES(?,'HASHEOUS',?,?,?,?,?,?,?)`,
		value.ID,
		value.RequestDigest,
		status,
		value.Outcome,
		fileRecord,
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
 (provider,request_digest,current_response_id,expires_at_ms,updated_at_ms) VALUES('HASHEOUS',
?,?,?,?)
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
