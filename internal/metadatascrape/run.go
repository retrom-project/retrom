package metadatascrape

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/hasheous"
	lookuppersistence "retrom/internal/persistence/metadatascrape"
	lookupservice "retrom/internal/service/metadatascrape"
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
			ctx, runID, item.id, resolved.Result, resolved.CachedResponseID, attempt, allowCandidate,
		)
		if err != nil {
			return false, "METADATA_PERSIST_FAILED", err
		}
		if resolved.CachedResponseID != "" || !retryableOutcome(resolved.Result.Outcome) || attempt == 3 {
			return created, "", nil
		}
		if err := waitRetry(ctx, time.Duration(100*(1<<(attempt-1)))*time.Millisecond); err != nil {
			return false, "METADATA_CANCELLED", err
		}
	}
	return false, "", nil
}

func (service *Service) lookup(
	ctx context.Context,
	hashes hasheous.ContentHashes,
	bypassCache bool,
) (lookupservice.ResolvedLookup, error) {
	lookup := lookupservice.NewLookup(
		lookuppersistence.NewCache(
			service.database,
		),
		service.blobs,
		service.provider,
		service.now,
	)
	result, err := lookup.Lookup(ctx, hashes, bypassCache)
	if err != nil {
		return lookupservice.ResolvedLookup{}, fmt.Errorf("resolve scrape lookup: %w", err)
	}
	return result, nil
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

func (service *Service) persistResult(
	ctx context.Context,
	runID, evidenceID string,
	result hasheous.LookupResult,
	cachedResponseID string,
	attemptNo int,
	allowCandidate bool,
) (bool, error) {
	recorder := lookupservice.NewRecorder(lookuppersistence.NewRecorder(service.database), service.blobs, service.now)
	created, err := recorder.Record(ctx, lookupservice.LookupAttempt{
		RunID: runID, EvidenceID: evidenceID,
		Lookup:    lookupservice.ResolvedLookup{Result: result, CachedResponseID: cachedResponseID},
		AttemptNo: attemptNo, AllowCandidate: allowCandidate,
	})
	if err != nil {
		return false, fmt.Errorf("record metadata lookup: %w", err)
	}
	return created, nil
}
