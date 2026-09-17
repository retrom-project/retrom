package metadatascrape

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/metadatascrape"

	"retrom/internal/adapter/files/blobstore"
)

type ResultRecorder struct {
	repository model.ResultRepository
	blobs      model.AssetBlobs
	now        func() time.Time
}

func NewRecorder(repository model.ResultRepository, blobs model.AssetBlobs, now func() time.Time) *ResultRecorder {
	return &ResultRecorder{repository: repository, blobs: blobs, now: now}
}

func (recorder *ResultRecorder) Record(ctx context.Context, attempt model.LookupAttempt) (bool, error) {
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
	result, err := recorder.repository.CommitRecord(ctx, model.RecordCommand{
		Attempt:   attempt,
		Blob:      blob,
		Candidate: candidate,
		Now:       recorder.now().UnixMilli(),
	})
	if err != nil {
		return false, fmt.Errorf("commit scrape result: %w", err)
	}
	return result.Created, nil
}

func prepareCandidate(attempt model.LookupAttempt) (*model.RecordCandidate, error) {
	if !attempt.AllowCandidate || attempt.Lookup.Result.Candidate == nil {
		return nil, nil //nolint:nilnil // absence is a valid non-error outcome
	}
	c := attempt.Lookup.Result.Candidate
	metadata, err := json.Marshal(c.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode candidate metadata: %w", err)
	}
	evidence, err := json.Marshal(c.Evidence)
	if err != nil {
		return nil, fmt.Errorf("encode candidate evidence: %w", err)
	}
	return &model.RecordCandidate{
		MetadataJSON: string(metadata), EvidenceJSON: string(evidence), Value: c,
	}, nil
}

func (recorder *ResultRecorder) prepareRaw(lookup model.ResolvedLookup) (*blobstore.Metadata, error) {
	if lookup.CachedResponseID != "" || len(lookup.Result.RawResponse) == 0 {
		return nil, nil //nolint:nilnil // absence is a valid non-error outcome
	}
	blob, err := recorder.blobs.Put(bytes.NewReader(lookup.Result.RawResponse))
	if err != nil {
		return nil, fmt.Errorf("store raw scrape response: %w", err)
	}
	return &blob, nil
}
