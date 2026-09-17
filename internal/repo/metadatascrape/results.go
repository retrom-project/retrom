package metadatascrape

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"

	"github.com/google/uuid"
)

type (
	ResultRepository struct {
		database      *sql.DB
		preCommitHook func() error
	}
	resultRecords struct{ transaction *sql.Tx }
)

func NewRecorder(database *sql.DB) *ResultRepository { return &ResultRepository{database: database} }

func WithResultPreCommitHook(repo *ResultRepository, hook func() error) {
	repo.preCommitHook = hook
}

func (repository *ResultRepository) CommitRecord(
	ctx context.Context, cmd metadatascrape.RecordCommand,
) (metadatascrape.RecordResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return metadatascrape.RecordResult{}, fmt.Errorf("begin scrape result: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := resultRecords{tx}
	claim := cmd.Attempt.Claim
	claim.Now = cmd.Now

	writable, err := records.Writable(ctx, claim)
	if err != nil {
		return metadatascrape.RecordResult{}, fmt.Errorf("read scrape result owner: %w", err)
	}
	if !writable {
		return metadatascrape.RecordResult{}, metadatascrape.ErrExecutionLost
	}

	responseID, source, err := recordResultResponse(ctx, records, cmd.Attempt.Lookup, cmd.Blob, cmd.Now)
	if err != nil {
		return metadatascrape.RecordResult{}, err
	}
	attemptID, err := resultUUID()
	if err != nil {
		return metadatascrape.RecordResult{}, err
	}
	if err := records.Attempt(ctx, metadatascrape.AttemptRecord{
		ID: attemptID, RunID: cmd.Attempt.Claim.RunID, EvidenceID: cmd.Attempt.EvidenceID,
		ResponseID: responseID, Source: source, AttemptNo: cmd.Attempt.AttemptNo, Now: cmd.Now,
	}); err != nil {
		return metadatascrape.RecordResult{}, fmt.Errorf("record scrape attempt: %w", err)
	}

	created := false
	if cmd.Candidate != nil && cmd.Candidate.Value != nil {
		var recordErr error
		created, recordErr = recordResultCandidate(ctx, records, cmd, responseID, attemptID)
		if recordErr != nil {
			return metadatascrape.RecordResult{}, recordErr
		}
	}

	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return metadatascrape.RecordResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return metadatascrape.RecordResult{}, fmt.Errorf("commit scrape result records: %w", err)
	}
	return metadatascrape.RecordResult{Created: created}, nil
}

func resultUUID() (string, error) {
	v, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create result identity: %w", err)
	}
	return v.String(), nil
}

func recordResultResponse(
	ctx context.Context,
	records resultRecords,
	lookup metadatascrape.ResolvedLookup,
	blob *blobstore.Metadata,
	now int64,
) (string, string, error) {
	if lookup.CachedResponseID != "" {
		return lookup.CachedResponseID, "CACHE", nil
	}
	id, err := resultUUID()
	if err != nil {
		return "", "", err
	}
	result := lookup.Result
	err = records.Response(ctx, metadatascrape.ResponseRecord{
		ID: id, RequestDigest: result.RequestDigest, Outcome: result.Outcome,
		HTTPStatus: result.HTTPStatus, Blob: blob,
		Cacheable: result.Outcome == hasheous.OutcomeHit || result.Outcome == hasheous.OutcomeMiss,
		Now:       now, ExpiresAt: resultResponseExpiry(result.Outcome, now),
	})
	if err != nil {
		return "", "", fmt.Errorf("record provider response: %w", err)
	}
	return id, "NETWORK", nil
}

func resultResponseExpiry(outcome hasheous.ProviderOutcome, now int64) int64 {
	switch outcome { //nolint:exhaustive // transient outcomes use the default short expiry
	case hasheous.OutcomeHit:
		return now + 7*24*60*60*1000
	case hasheous.OutcomeMiss:
		return now + 24*60*60*1000
	default:
		return now
	}
}

func recordResultCandidate(
	ctx context.Context,
	records resultRecords,
	cmd metadatascrape.RecordCommand,
	responseID, attemptID string,
) (bool, error) {
	prepared := cmd.Candidate
	id, err := resultUUID()
	if err != nil {
		return false, err
	}
	candidate, err := records.Candidate(ctx, metadatascrape.CandidateRecord{
		ID: id, RunID: cmd.Attempt.Claim.RunID, ResponseID: responseID,
		ProviderGameID: prepared.Value.ProviderGameID,
		MetadataJSON:   prepared.MetadataJSON, EvidenceJSON: prepared.EvidenceJSON, Now: cmd.Now,
	})
	if err != nil {
		return false, fmt.Errorf("record scrape candidate: %w", err)
	}
	hashes, err := records.Hashes(ctx, cmd.Attempt.EvidenceID)
	if err != nil {
		return false, fmt.Errorf("read candidate hash evidence: %w", err)
	}
	encoded, err := resultMatchedHashes(hashes)
	if err != nil {
		return false, err
	}
	if err := records.Hit(ctx, metadatascrape.CandidateHit{
		CandidateID: candidate.ID, AttemptID: attemptID, HashesJSON: encoded, Now: cmd.Now,
	}); err != nil {
		return false, fmt.Errorf("record candidate hit: %w", err)
	}
	if !candidate.Created {
		return false, nil
	}
	assets := make([]metadatascrape.CandidateAsset, 0, len(prepared.Value.Assets))
	for _, asset := range prepared.Value.Assets {
		assetID, err := resultUUID()
		if err != nil {
			return false, err
		}
		assets = append(assets, metadatascrape.CandidateAsset{
			ID: assetID, CandidateID: candidate.ID, ResponseID: responseID,
			Reference: asset, Now: cmd.Now,
		})
	}
	for i := range assets {
		if err := enqueueResultMedia(ctx, records, cmd.Attempt.Claim.RunID, &assets[i]); err != nil {
			return false, err
		}
	}
	if err := records.Assets(ctx, assets); err != nil {
		return false, fmt.Errorf("record candidate assets: %w", err)
	}
	return true, nil
}

func resultMatchedHashes(hashes metadatascrape.Hashes) (string, error) {
	matched := make(map[string]string, 4)
	for name, value := range map[string]*string{
		"crc32": hashes.CRC32, "md5": hashes.MD5, "sha1": hashes.SHA1, "sha256": hashes.SHA256,
	} {
		if value != nil {
			matched[name] = *value
		}
	}
	encoded, err := json.Marshal(matched)
	if err != nil {
		return "", fmt.Errorf("encode matched hashes: %w", err)
	}
	return string(encoded), nil
}

func enqueueResultMedia(
	ctx context.Context,
	records resultRecords,
	runID string,
	asset *metadatascrape.CandidateAsset,
) error {
	subject, err := records.Subject(ctx, runID)
	if err != nil {
		return fmt.Errorf("read media owner: %w", err)
	}
	plan, err := buildMediaJob(subject, runID, *asset)
	if err != nil {
		return err
	}
	if err := records.Enqueue(ctx, plan); err != nil {
		return fmt.Errorf("enqueue media fetch: %w", err)
	}
	asset.MediaJobID = plan.JobID
	return nil
}

func buildMediaJob(
	scope metadatascrape.Subject, runID string, asset metadatascrape.CandidateAsset,
) (metadatascrape.MediaJobPlan, error) {
	jobID, err := resultUUID()
	if err != nil {
		return metadatascrape.MediaJobPlan{}, err
	}
	executionID, err := resultUUID()
	if err != nil {
		return metadatascrape.MediaJobPlan{}, err
	}
	envelope := metadatascrape.MediaInputEnvelope{
		SchemaVersion: 1, Kind: "MEDIA_FETCH",
		Scope:       metadatascrape.MediaInputScope{Type: scope.Kind, ID: scope.ID},
		ExecutionID: executionID,
		Inputs: metadatascrape.MediaInput{
			AssetID: asset.ID, RunID: runID, ResponseID: asset.ResponseID,
			SourceDigest: resultMediaSourceDigest(asset),
		},
	}
	input, err := json.Marshal(envelope)
	if err != nil {
		return metadatascrape.MediaJobPlan{}, fmt.Errorf("encode media execution input: %w", err)
	}
	canonical, err := json.Marshal(map[string]string{"candidateAssetId": asset.ID})
	if err != nil {
		return metadatascrape.MediaJobPlan{}, fmt.Errorf("encode media job identity: %w", err)
	}
	digest := sha256.Sum256(input)
	dedupe := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00MEDIA_FETCH\x00"), canonical...))
	return metadatascrape.MediaJobPlan{
		JobID: jobID, RunID: runID, AssetID: asset.ID, Scope: scope, Now: asset.Now,
		InputJSON: string(input), InputDigest: hex.EncodeToString(digest[:]),
		Dedupe: hex.EncodeToString(dedupe[:]),
	}, nil
}

func resultMediaSourceDigest(asset metadatascrape.CandidateAsset) string {
	source := sha256.New()
	for _, field := range []string{
		asset.ID, asset.CandidateID, asset.ResponseID, asset.Reference.ProviderAssetID,
		asset.Reference.Path, asset.Reference.Kind, fmt.Sprint(asset.Reference.Ordinal),
	} {
		_, _ = fmt.Fprintf(source, "%d:%s", len(field), field)
	}
	return hex.EncodeToString(source.Sum(nil))
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
