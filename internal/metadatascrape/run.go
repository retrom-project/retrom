package metadatascrape

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/hasheous"
)

type scrapeEvidence struct {
	id                       string
	crc32, md5, sha1, sha256 sql.NullString
}

func (service *Service) Run(ctx context.Context, runID string) error {
	var jobID, providerName, state, payloadJSON string
	if err := service.database.QueryRowContext(ctx, `
SELECT r.job_id,
r.provider,
r.state,
j.payload_json
FROM metadata_scrape_runs r
JOIN jobs j ON j.id=r.job_id
WHERE r.id=?
`, runID).Scan(&jobID, &providerName, &state, &payloadJSON); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if providerName == "NONE" || state != "RUNNING" {
		return nil
	}
	var payload struct {
		BypassCache bool `json:"bypassCache"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_JOB_PAYLOAD_INVALID", err)
	}
	now := service.now().UnixMilli()
	if _, err := service.database.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=?,
execution_deadline_at_ms=?,
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='in-process',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`, now, now+15_000, now+60_000, now, now, jobID); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_JOB_START_FAILED", err)
	}
	if _, err := service.database.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'STARTED',
'{}',
?
FROM jobs
WHERE id=?
`, now, jobID); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_EVENT_FAILED", err)
	}
	evidenceList, err := service.loadScrapeEvidence(ctx, runID)
	if err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_EVIDENCE_FAILED", err)
	}
	candidateCount, code, err := service.processScrapeEvidence(ctx, runID, evidenceList, payload.BypassCache)
	if err != nil {
		return service.fail(ctx, runID, jobID, code, err)
	}
	if err := service.fetchPendingAssets(ctx, runID); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_ASSET_PERSIST_FAILED", err)
	}
	return service.complete(ctx, runID, jobID, candidateCount)
}

func (service *Service) loadScrapeEvidence(ctx context.Context, runID string) ([]scrapeEvidence, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,crc32,md5,sha1,sha256 FROM content_hash_evidence
WHERE scrape_run_id=? ORDER BY query_order,id LIMIT 8
`, runID)
	if err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	evidenceList := make([]scrapeEvidence, 0, 8)
	for rows.Next() {
		var value scrapeEvidence
		if err := rows.Scan(&value.id, &value.crc32, &value.md5, &value.sha1, &value.sha256); err != nil {
			return nil, fmt.Errorf("metadatascrape/service: %w", err)
		}
		evidenceList = append(evidenceList, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	return evidenceList, nil
}

func (service *Service) processScrapeEvidence(
	ctx context.Context,
	runID string,
	evidenceList []scrapeEvidence,
	bypassCache bool,
) (int, string, error) {
	candidateCount := 0
	for _, item := range evidenceList {
		created, code, err := service.processEvidenceItem(ctx, runID, item, bypassCache, candidateCount < 20)
		if err != nil {
			return 0, code, err
		}
		if created {
			candidateCount++
		}
	}
	return candidateCount, "", nil
}

func (service *Service) processEvidenceItem(
	ctx context.Context,
	runID string,
	item scrapeEvidence,
	bypassCache, allowCandidate bool,
) (bool, string, error) {
	hashes := hasheous.ContentHashes{
		CRC32: item.crc32.String, MD5: item.md5.String, SHA1: item.sha1.String, SHA256: item.sha256.String,
	}
	for attempt := 1; attempt <= 3; attempt++ {
		resolved, err := service.lookup(ctx, hashes, bypassCache)
		if err != nil {
			return false, "METADATA_REQUEST_INVALID", err
		}
		created, err := service.persistResult(
			ctx, runID, item.id, resolved.result, resolved.cachedResponseID, attempt, allowCandidate,
		)
		if err != nil {
			return false, "METADATA_PERSIST_FAILED", err
		}
		if resolved.cachedResponseID != "" || !retryableOutcome(resolved.result.Outcome) || attempt == 3 {
			return created, "", nil
		}
		if err := waitRetry(ctx, time.Duration(100*(1<<(attempt-1)))*time.Millisecond); err != nil {
			return false, "METADATA_CANCELLED", err
		}
	}
	return false, "", nil
}

type resolvedLookup struct {
	result           hasheous.LookupResult
	cachedResponseID string
}

func (service *Service) lookup(
	ctx context.Context,
	hashes hasheous.ContentHashes,
	bypassCache bool,
) (resolvedLookup, error) {
	digest, err := hasheous.RequestDigest(hashes)
	if err != nil {
		return resolvedLookup{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if !bypassCache {
		cached, found, err := service.cachedLookup(ctx, digest, hashes)
		if err != nil {
			return resolvedLookup{}, err
		}
		if found {
			return cached, nil
		}
	}
	result, err := service.provider.LookupByHash(ctx, hashes)
	if err != nil {
		return resolvedLookup{}, fmt.Errorf("look up metadata by hash: %w", err)
	}
	return resolvedLookup{result: result}, nil
}

func (service *Service) cachedLookup(
	ctx context.Context,
	digest string,
	hashes hasheous.ContentHashes,
) (resolvedLookup, bool, error) {
	var responseID, outcome string
	var status sql.NullInt64
	var rawSHA sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT r.id,r.outcome,r.http_status,b.sha256 FROM metadata_provider_cache c
JOIN metadata_provider_responses r ON r.id=c.current_response_id
LEFT JOIN blobs b ON b.id=r.raw_response_blob_id
WHERE c.provider='HASHEOUS' AND c.request_digest=? AND c.expires_at_ms>?
`, digest, service.now().UnixMilli()).Scan(&responseID, &outcome, &status, &rawSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return resolvedLookup{}, false, nil
	}
	if err != nil {
		return resolvedLookup{}, false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	raw := service.readCachedResponse(rawSHA)
	if outcome != string(hasheous.OutcomeMiss) && len(raw) == 0 {
		return resolvedLookup{}, false, nil
	}
	if restored, restoreErr := service.provider.RestoreCached(
		hashes, hasheous.ProviderOutcome(outcome), int(status.Int64), raw,
	); restoreErr == nil {
		return resolvedLookup{result: restored, cachedResponseID: responseID}, true, nil
	}
	return resolvedLookup{}, false, nil
}

func (service *Service) readCachedResponse(rawSHA sql.NullString) []byte {
	if !rawSHA.Valid {
		return nil
	}
	file, err := service.blobs.OpenDigest(rawSHA.String)
	if err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	cleanup.Error("close", file.Close())
	if err != nil || len(raw) > 4<<20 {
		return nil
	}
	return raw
}

func retryableOutcome(outcome hasheous.ProviderOutcome) bool {
	return outcome == hasheous.OutcomeRateLimited || outcome == hasheous.OutcomeTimeout ||
		outcome == hasheous.OutcomeNetworkError
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("metadatascrape/service: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (service *Service) persistProviderResponse(
	ctx context.Context,
	transaction *sql.Tx,
	result hasheous.LookupResult,
	cachedResponseID string,
	now int64,
) (string, string, error) {
	if cachedResponseID != "" {
		return cachedResponseID, "CACHE", nil
	}
	rawBlobIDValue, err := service.persistRawResponse(ctx, transaction, result.RawResponse, now)
	if err != nil {
		return "", "", err
	}
	var rawBlobID any
	if rawBlobIDValue != "" {
		rawBlobID = rawBlobIDValue
	}
	responseID := newID()
	expiresAt := service.providerResponseExpiry(result.Outcome, now)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_provider_responses(id,provider,request_digest,http_status,outcome,
raw_response_blob_id,fetched_at_ms,expires_at_ms) VALUES(?,'HASHEOUS',?,?,?,?,?,?)
`, responseID, result.RequestDigest, nullableStatus(result.HTTPStatus), string(result.Outcome),
		rawBlobID, now, expiresAt); err != nil {
		return "", "", fmt.Errorf("metadatascrape/service: %w", err)
	}
	if result.Outcome == hasheous.OutcomeHit || result.Outcome == hasheous.OutcomeMiss {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_provider_cache(provider,request_digest,current_response_id,expires_at_ms,updated_at_ms)
VALUES('HASHEOUS',?,?,?,?) ON CONFLICT(provider,request_digest)
DO UPDATE SET current_response_id=excluded.current_response_id,
expires_at_ms=excluded.expires_at_ms,updated_at_ms=excluded.updated_at_ms
`, result.RequestDigest, responseID, expiresAt, now); err != nil {
			return "", "", fmt.Errorf("metadatascrape/service: %w", err)
		}
	}
	return responseID, "NETWORK", nil
}

func (service *Service) persistRawResponse(
	ctx context.Context,
	transaction *sql.Tx,
	raw []byte,
	now int64,
) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	metadata, err := service.blobs.Put(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("metadatascrape/service: %w", err)
	}
	blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, "application/json", now)
	if err != nil {
		return "", fmt.Errorf("metadatascrape/service: %w", err)
	}
	return blobID, nil
}

func (service *Service) providerResponseExpiry(outcome hasheous.ProviderOutcome, now int64) int64 {
	switch outcome {
	case hasheous.OutcomeHit:
		return service.now().Add(7 * 24 * time.Hour).UnixMilli()
	case hasheous.OutcomeMiss:
		return service.now().Add(24 * time.Hour).UnixMilli()
	case hasheous.OutcomeRateLimited, hasheous.OutcomeTimeout,
		hasheous.OutcomeInvalidResponse, hasheous.OutcomeNetworkError:
		return now
	}
	return now
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) persistResult(
	ctx context.Context,
	runID, evidenceID string,
	result hasheous.LookupResult,
	cachedResponseID string,
	attemptNo int,
	allowCandidate bool,
) (bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	responseID, source, err := service.persistProviderResponse(ctx, transaction, result, cachedResponseID, now)
	if err != nil {
		return false, err
	}
	attemptID := newID()
	if source == "CACHE" {
		attemptNo = 1
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_scrape_query_attempts(id,
scrape_run_id,
content_hash_evidence_id,
provider_response_id,
attempt_no,
source,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?)
`, attemptID, runID, evidenceID, responseID, attemptNo, source, now); err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if result.Candidate == nil || !allowCandidate {
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf("commit metadata lookup without candidate: %w", err)
		}
		return false, nil
	}
	return service.persistCandidate(ctx, transaction, runID, evidenceID, responseID, attemptID, result, now)
}

func (service *Service) persistCandidate(
	ctx context.Context,
	transaction *sql.Tx,
	runID, evidenceID, responseID, attemptID string,
	result hasheous.LookupResult,
	now int64,
) (bool, error) {
	metadataJSON, _ := json.Marshal(result.Candidate.Metadata)
	evidenceJSON, _ := json.Marshal(result.Candidate.Evidence)
	candidateID := newID()
	resultInsert, err := transaction.ExecContext(
		ctx,
		`
INSERT INTO scrape_candidates(id,
scrape_run_id,
primary_response_id,
provider_game_id,
normalized_metadata_json,
evidence_json,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?) ON CONFLICT(scrape_run_id,
provider_game_id) DO NOTHING
`,
		candidateID,
		runID,
		responseID,
		result.Candidate.ProviderGameID,
		string(metadataJSON),
		string(evidenceJSON),
		now,
	)
	if err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	inserted, _ := resultInsert.RowsAffected()
	if inserted == 0 {
		if err := transaction.QueryRowContext(ctx, `
SELECT id
FROM scrape_candidates
WHERE scrape_run_id=?
AND provider_game_id=?
`, runID, result.Candidate.ProviderGameID).Scan(&candidateID); err != nil {
			return false, fmt.Errorf("metadatascrape/service: %w", err)
		}
	}
	var crc32Value, md5Value, sha1Value, sha256Value sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT crc32,
md5,
sha1,
sha256
FROM content_hash_evidence
WHERE id=?
`, evidenceID).Scan(&crc32Value, &md5Value, &sha1Value, &sha256Value); err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	matched := make(map[string]string, 4)
	hashes := map[string]sql.NullString{
		"crc32": crc32Value, "md5": md5Value, "sha1": sha1Value, "sha256": sha256Value,
	}
	for key, value := range hashes {
		if value.Valid {
			matched[key] = value.String
		}
	}
	matchedJSON, _ := json.Marshal(matched)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO scrape_candidate_hits(scrape_candidate_id,
query_attempt_id,
matched_hashes_json,
created_at_ms) VALUES(?,
?,
?,
?)
`, candidateID, attemptID, string(matchedJSON), now); err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if inserted == 1 {
		for _, asset := range result.Candidate.Assets {
			assetID := newID()
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO scrape_candidate_assets(id,
scrape_candidate_id,
provider_response_id,
provider_asset_id,
kind_hint,
ordinal,
source_path,
status,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
'PENDING',
1,
?,
?)
`,
				assetID,
				candidateID,
				responseID,
				asset.ProviderAssetID,
				asset.Kind,
				asset.Ordinal,
				asset.Path,
				now,
				now,
			); err != nil {
				return false, fmt.Errorf("metadatascrape/service: %w", err)
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit metadata candidate: %w", err)
	}
	return inserted == 1, nil
}
