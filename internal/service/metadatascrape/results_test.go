package metadatascrape

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	model "retrom/internal/model/metadatascrape"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
)

type resultMemory struct {
	inTransaction bool
	calls         int
	lateError     error
	readable      bool
	response      model.ResponseRecord
	responses     int
	attempt       model.AttemptRecord
	candidate     model.CandidateIdentity
	hit           model.CandidateHit
	assets        []model.CandidateAsset
}

func (memory *resultMemory) CommitWrite(_ context.Context, work func(model.ResultScope) error) error {
	memory.calls++
	memory.inTransaction = true
	defer func() { memory.inTransaction = false }()
	if err := work(model.ResultScope{Read: memory, Write: memory, Media: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *resultMemory) Writable(context.Context, model.WorkerClaim) (bool, error) {
	return memory.readable, nil
}

func (memory *resultMemory) Hashes(context.Context, string) (model.Hashes, error) {
	value := "sha1"
	return model.Hashes{SHA1: &value}, nil
}

func (memory *resultMemory) Response(_ context.Context, value model.ResponseRecord) error {
	memory.response = value
	memory.responses++
	return nil
}

func (memory *resultMemory) Attempt(_ context.Context, value model.AttemptRecord) error {
	memory.attempt = value
	return nil
}

func (memory *resultMemory) Candidate(_ context.Context, value model.CandidateRecord) (model.CandidateIdentity, error) {
	if memory.candidate.Created {
		memory.candidate.ID = value.ID
	}
	return memory.candidate, nil
}

func (memory *resultMemory) Hit(_ context.Context, value model.CandidateHit) error {
	memory.hit = value
	return nil
}

func (memory *resultMemory) Assets(_ context.Context, values []model.CandidateAsset) error {
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
	created, err := recorder.Record(t.Context(), model.LookupAttempt{
		Claim: model.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 2,
		Lookup: model.ResolvedLookup{Result: hasheous.LookupResult{Outcome: hasheous.OutcomeMiss, RawResponse: []byte("raw")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || blobs.calls != 1 || records.responses != 1 || !records.response.Cacheable || records.response.ExpiresAt != 100+86400000 || records.attempt.Source != "NETWORK" || records.attempt.AttemptNo != 2 {
		t.Fatalf("response recording: %+v attempt=%+v", records.response, records.attempt)
	}
}

func TestCachedCandidateHitReusesResponseAndDoesNotDuplicateAssets(t *testing.T) {
	records := &resultMemory{readable: true, candidate: model.CandidateIdentity{ID: "existing"}}
	blobs := &responseBlobs{t: t, records: records}
	recorder := NewRecorder(records, blobs, func() time.Time { return time.UnixMilli(100) })
	created, err := recorder.Record(t.Context(), model.LookupAttempt{
		Claim: model.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 3, AllowCandidate: true,
		Lookup: model.ResolvedLookup{CachedResponseID: "cached", Result: hasheous.LookupResult{
			Outcome: hasheous.OutcomeHit, RawResponse: []byte("raw"),
			Candidate: &hasheous.Candidate{ProviderGameID: "provider-game", Metadata: map[string]any{"title": "title"}, Assets: []hasheous.AssetRef{{ProviderAssetID: "asset"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || blobs.calls != 0 || records.responses != 0 || records.attempt.ResponseID != "cached" || records.attempt.Source != "CACHE" || records.attempt.AttemptNo != 3 || records.hit.CandidateID != "existing" || len(records.assets) != 0 {
		t.Fatalf("cached hit: %+v / %+v", records.attempt, records.hit)
	}
	if records.hit.HashesJSON != `{"sha1":"sha1"}` {
		t.Fatalf("fabricated hashes: %s", records.hit.HashesJSON)
	}
}

func TestInvalidCandidateCannotReachPersistence(t *testing.T) {
	records := &resultMemory{readable: true}
	recorder := NewRecorder(records, &responseBlobs{t: t, records: records}, time.Now)
	_, err := recorder.Record(t.Context(), model.LookupAttempt{AllowCandidate: true, Lookup: model.ResolvedLookup{Result: hasheous.LookupResult{
		Candidate: &hasheous.Candidate{Metadata: map[string]any{"invalid": math.NaN()}},
	}}})
	var invalid *json.UnsupportedValueError
	if !errors.As(err, &invalid) || records.calls != 0 {
		t.Fatalf("invalid candidate persisted: calls=%d error=%v", records.calls, err)
	}
}

func TestResultCommitFailureDoesNotReportCreatedCandidate(t *testing.T) {
	records := &resultMemory{readable: true, candidate: model.CandidateIdentity{Created: true}, lateError: context.DeadlineExceeded}
	recorder := NewRecorder(records, &responseBlobs{t: t, records: records}, time.Now)
	created, err := recorder.Record(t.Context(), model.LookupAttempt{
		Claim: model.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 1, AllowCandidate: true,
		Lookup: model.ResolvedLookup{CachedResponseID: "cached", Result: hasheous.LookupResult{Candidate: &hasheous.Candidate{ProviderGameID: "game"}}},
	})
	if created || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failed commit returned success: %t / %v", created, err)
	}
	if records.hit.CandidateID == "" {
		t.Fatal("late failure did not execute candidate writes")
	}
}

func (memory *resultMemory) Subject(context.Context, string) (model.Subject, error) {
	return model.Subject{Kind: "IMPORT_ITEM", ID: "item"}, nil
}
func (memory *resultMemory) Enqueue(context.Context, model.MediaJobPlan) error { return nil }
