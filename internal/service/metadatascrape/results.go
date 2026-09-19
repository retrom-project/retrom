package metadatascrape

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/model/blob"
	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type ResultRecorder struct {
	repository metadatascrapemodel.ResultRepository
	blobs      metadatascrapemodel.AssetBlobs
	now        func() time.Time
}

func NewRecorder(
	repository metadatascrapemodel.ResultRepository,
	blobs metadatascrapemodel.AssetBlobs,
	now func() time.Time,
) *ResultRecorder {
	return &ResultRecorder{repository: repository, blobs: blobs, now: now}
}

type preparedRaw struct {
	blob *blob.PreparedBlob
}

type preparedCandidate struct {
	metadata, evidence string
	value              *metadatamodel.Candidate
}

func prepareCandidate(attempt metadatascrapemodel.LookupAttempt) (preparedCandidate, error) {
	if !attempt.AllowCandidate || attempt.Lookup.Result.Candidate == nil {
		return preparedCandidate{}, nil
	}
	candidate := attempt.Lookup.Result.Candidate
	metadata, err := json.Marshal(candidate.Metadata)
	if err != nil {
		return preparedCandidate{}, fmt.Errorf("encode candidate metadata: %w", err)
	}
	evidence, err := json.Marshal(candidate.Evidence)
	if err != nil {
		return preparedCandidate{}, fmt.Errorf("encode candidate evidence: %w", err)
	}
	return preparedCandidate{metadata: string(metadata), evidence: string(evidence), value: candidate}, nil
}

func (recorder *ResultRecorder) Record(ctx context.Context, attempt metadatascrapemodel.LookupAttempt) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("record scrape result: %w", err)
	}
	candidate, err := prepareCandidate(attempt)
	if err != nil {
		return false, err
	}
	blob, err := recorder.prepareRaw(attempt.Lookup)
	if err != nil {
		return false, err
	}
	now := recorder.now().UnixMilli()
	created := false
	err = recorder.repository.WithWrite(ctx, func(scope metadatascrapemodel.ResultScope) error {
		claim := attempt.Claim
		claim.Now = recorder.now().UnixMilli()
		writable, err := scope.Read.Writable(ctx, claim)
		if err != nil {
			return fmt.Errorf("read scrape result owner: %w", err)
		}
		if !writable {
			return metadatascrapemodel.ErrExecutionLost
		}
		responseID, source, err := recordResponse(ctx, scope.Write, attempt.Lookup, blob.blob, now)
		if err != nil {
			return err
		}
		attemptID, err := scheduleID()
		if err != nil {
			return err
		}
		number := attempt.AttemptNo
		if err := scope.Write.Attempt(ctx, metadatascrapemodel.AttemptRecord{
			ID: attemptID, RunID: attempt.Claim.RunID, EvidenceID: attempt.EvidenceID,
			ResponseID: responseID, Source: source, AttemptNo: number, Now: now,
		}); err != nil {
			return fmt.Errorf("record scrape attempt: %w", err)
		}
		if candidate.value == nil {
			return nil
		}
		created, err = recordCandidate(ctx, scope, attempt, candidate, responseID, attemptID, now)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("commit scrape result: %w", err)
	}
	return created, nil
}

func (recorder *ResultRecorder) prepareRaw(lookup metadatascrapemodel.ResolvedLookup) (preparedRaw, error) {
	if lookup.CachedResponseID != "" || len(lookup.Result.RawResponse) == 0 {
		return preparedRaw{}, nil
	}
	blob, err := recorder.blobs.Put(bytes.NewReader(lookup.Result.RawResponse))
	if err != nil {
		return preparedRaw{}, fmt.Errorf("store raw scrape response: %w", err)
	}
	return preparedRaw{blob: &blob}, nil
}

func recordResponse(
	ctx context.Context,
	writer metadatascrapemodel.ResultWriter, lookup metadatascrapemodel.ResolvedLookup, blob *blob.PreparedBlob,
	now int64,
) (string, string, error) {
	if lookup.CachedResponseID != "" {
		return lookup.CachedResponseID, "CACHE", nil
	}
	id, err := scheduleID()
	if err != nil {
		return "", "", err
	}
	result := lookup.Result
	err = writer.Response(ctx, metadatascrapemodel.ResponseRecord{
		ID: id, RequestDigest: result.RequestDigest, Outcome: result.Outcome, Audit: result.Audit, Blob: blob,
		Cacheable: result.Outcome == metadatamodel.OutcomeHit || result.Outcome == metadatamodel.OutcomeMiss,
		Now:       now, ExpiresAt: ResponseExpiry(
			result.Outcome,
			now,
		),
	})
	if err != nil {
		return "", "", fmt.Errorf("record provider response: %w", err)
	}
	return id, "NETWORK", nil
}
