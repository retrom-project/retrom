package metadatascrape

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/hasheous"
)

type resultMemory struct {
	inTransaction bool
	calls         int
	lateError     error
	readable      bool
	response      ResponseRecord
	responses     int
	attempt       AttemptRecord
	candidate     CandidateIdentity
	hit           CandidateHit
	assets        []CandidateAsset
}

func (memory *resultMemory) WithWrite(_ context.Context, work func(ResultScope) error) error {
	memory.calls++
	memory.inTransaction = true
	defer func() { memory.inTransaction = false }()
	if err := work(ResultScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *resultMemory) Writable(context.Context, WorkerClaim) (bool, error) {
	return memory.readable, nil
}

func (memory *resultMemory) Hashes(context.Context, string) (Hashes, error) {
	value := "sha1"
	return Hashes{SHA1: &value}, nil
}

func (memory *resultMemory) Response(_ context.Context, value ResponseRecord) error {
	memory.response = value
	memory.responses++
	return nil
}

func (memory *resultMemory) Attempt(_ context.Context, value AttemptRecord) error {
	memory.attempt = value
	return nil
}

func (memory *resultMemory) Candidate(_ context.Context, value CandidateRecord) (CandidateIdentity, error) {
	if memory.candidate.Created {
		memory.candidate.ID = value.ID
	}
	return memory.candidate, nil
}

func (memory *resultMemory) Hit(_ context.Context, value CandidateHit) error {
	memory.hit = value
	return nil
}

func (memory *resultMemory) Assets(_ context.Context, values []CandidateAsset) error {
	memory.assets = values
	return nil
}

type responseBlobs struct {
	t       *testing.T
	records *resultMemory
	calls   int
}

func (blobs *responseBlobs) Put(io.Reader) (blobstore.Metadata, error) {
	if blobs.records.inTransaction {
		blobs.t.Fatal("raw response file written inside SQL transaction")
	}
	blobs.calls++
	return blobstore.Metadata{SHA256: "raw", Size: 3}, nil
}

func TestRawResponseIsPreparedBeforeResultTransaction(t *testing.T) {
	records := &resultMemory{readable: true}
	blobs := &responseBlobs{t: t, records: records}
	recorder := NewRecorder(records, blobs, func() time.Time { return time.UnixMilli(100) })
	created, err := recorder.Record(t.Context(), LookupAttempt{
		Claim: WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 2,
		Lookup: ResolvedLookup{Result: hasheous.LookupResult{Outcome: hasheous.OutcomeMiss, RawResponse: []byte("raw")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || blobs.calls != 1 || records.responses != 1 || !records.response.Cacheable || records.response.ExpiresAt != 100+86400000 || records.attempt.Source != "NETWORK" || records.attempt.AttemptNo != 2 {
		t.Fatalf("response recording: %+v attempt=%+v", records.response, records.attempt)
	}
}

func TestCachedCandidateHitReusesResponseAndDoesNotDuplicateAssets(t *testing.T) {
	records := &resultMemory{readable: true, candidate: CandidateIdentity{ID: "existing"}}
	blobs := &responseBlobs{t: t, records: records}
	recorder := NewRecorder(records, blobs, func() time.Time { return time.UnixMilli(100) })
	created, err := recorder.Record(t.Context(), LookupAttempt{
		Claim: WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 3, AllowCandidate: true,
		Lookup: ResolvedLookup{CachedResponseID: "cached", Result: hasheous.LookupResult{
			Outcome: hasheous.OutcomeHit, RawResponse: []byte("raw"),
			Candidate: &hasheous.Candidate{ProviderGameID: "provider-game", Metadata: map[string]any{"title": "title"}, Assets: []hasheous.AssetRef{{ProviderAssetID: "asset"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || blobs.calls != 0 || records.responses != 0 || records.attempt.ResponseID != "cached" || records.attempt.Source != "CACHE" || records.attempt.AttemptNo != 1 || records.hit.CandidateID != "existing" || len(records.assets) != 0 {
		t.Fatalf("cached hit: %+v / %+v", records.attempt, records.hit)
	}
	if records.hit.HashesJSON != `{"sha1":"sha1"}` {
		t.Fatalf("fabricated hashes: %s", records.hit.HashesJSON)
	}
}

func TestInvalidCandidateCannotReachPersistence(t *testing.T) {
	records := &resultMemory{readable: true}
	recorder := NewRecorder(records, &responseBlobs{t: t, records: records}, time.Now)
	_, err := recorder.Record(t.Context(), LookupAttempt{AllowCandidate: true, Lookup: ResolvedLookup{Result: hasheous.LookupResult{
		Candidate: &hasheous.Candidate{Metadata: map[string]any{"invalid": math.NaN()}},
	}}})
	var invalid *json.UnsupportedValueError
	if !errors.As(err, &invalid) || records.calls != 0 {
		t.Fatalf("invalid candidate persisted: calls=%d error=%v", records.calls, err)
	}
}

func TestResultCommitFailureDoesNotReportCreatedCandidate(t *testing.T) {
	records := &resultMemory{readable: true, candidate: CandidateIdentity{Created: true}, lateError: context.DeadlineExceeded}
	recorder := NewRecorder(records, &responseBlobs{t: t, records: records}, time.Now)
	created, err := recorder.Record(t.Context(), LookupAttempt{
		Claim: WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 1, AllowCandidate: true,
		Lookup: ResolvedLookup{CachedResponseID: "cached", Result: hasheous.LookupResult{Candidate: &hasheous.Candidate{ProviderGameID: "game"}}},
	})
	if created || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failed commit returned success: %t / %v", created, err)
	}
	if records.hit.CandidateID == "" {
		t.Fatal("late failure did not execute candidate writes")
	}
}
