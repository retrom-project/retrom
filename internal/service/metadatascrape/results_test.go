package metadatascrape

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"retrom/internal/model/blob"
	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type resultMemory struct {
	inTransaction bool
	calls         int
	lateError     error
	readable      bool
	response      metadatascrapemodel.ResponseRecord
	responses     int
	attempt       metadatascrapemodel.AttemptRecord
	candidate     metadatascrapemodel.CandidateIdentity
	hit           metadatascrapemodel.CandidateHit
	assets        []metadatascrapemodel.CandidateAsset
}

func (memory *resultMemory) WithWrite(_ context.Context, work func(metadatascrapemodel.ResultScope) error) error {
	memory.calls++
	memory.inTransaction = true
	defer func() { memory.inTransaction = false }()
	if err := work(metadatascrapemodel.ResultScope{Read: memory, Write: memory, Media: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *resultMemory) Writable(context.Context, metadatascrapemodel.WorkerClaim) (bool, error) {
	return memory.readable, nil
}

func (memory *resultMemory) Hashes(context.Context, string) (metadatascrapemodel.Hashes, error) {
	value := "sha1"
	return metadatascrapemodel.Hashes{SHA1: &value}, nil
}

func (memory *resultMemory) Response(_ context.Context, value metadatascrapemodel.ResponseRecord) error {
	memory.response = value
	memory.responses++
	return nil
}

func (memory *resultMemory) Attempt(_ context.Context, value metadatascrapemodel.AttemptRecord) error {
	memory.attempt = value
	return nil
}

func (memory *resultMemory) Candidate(_ context.Context, value metadatascrapemodel.CandidateRecord) (metadatascrapemodel.CandidateIdentity, error) {
	if memory.candidate.Created {
		memory.candidate.ID = value.ID
	}
	return memory.candidate, nil
}

func (memory *resultMemory) Hit(_ context.Context, value metadatascrapemodel.CandidateHit) error {
	memory.hit = value
	return nil
}

func (memory *resultMemory) Assets(_ context.Context, values []metadatascrapemodel.CandidateAsset) error {
	memory.assets = values
	return nil
}

type responseBlobs struct {
	t       *testing.T
	records *resultMemory
	calls   int
	raw     []byte
}

func (blobs *responseBlobs) Put(reader io.Reader) (blob.PreparedBlob, error) {
	if blobs.records.inTransaction {
		blobs.t.Fatal("raw response file written inside SQL transaction")
	}
	contents, err := io.ReadAll(reader)
	if err != nil {
		return blob.PreparedBlob{}, err
	}
	blobs.calls++
	blobs.raw = contents
	return blob.PreparedBlob{SHA256: "raw", Size: int64(len(contents))}, nil
}

func TestRawResponseIsPreparedBeforeResultTransaction(t *testing.T) {
	records := &resultMemory{readable: true}
	blobs := &responseBlobs{t: t, records: records}
	recorder := NewRecorder(records, blobs, func() time.Time { return time.UnixMilli(100) })
	raw := []byte(" \n{\"known\":1,\"unknown\":{\"x\":[\"z\",null]}} \t")
	audit := metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":404}`)
	created, err := recorder.Record(t.Context(), metadatascrapemodel.LookupAttempt{
		Claim: metadatascrapemodel.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 2,
		Lookup: metadatascrapemodel.ResolvedLookup{Result: metadatamodel.LookupResult{Outcome: metadatamodel.OutcomeMiss, RawResponse: raw, Audit: audit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blobs.raw, raw) || !bytes.Equal(records.response.Audit, audit) {
		t.Fatalf("provider evidence changed: raw=%q audit=%q", blobs.raw, records.response.Audit)
	}
	if created || blobs.calls != 1 || records.responses != 1 || !records.response.Cacheable || records.response.ExpiresAt != 100+86400000 || records.attempt.Source != "NETWORK" || records.attempt.AttemptNo != 2 {
		t.Fatalf("response recording: %+v attempt=%+v", records.response, records.attempt)
	}
}

func TestCachedCandidateHitReusesResponseAndDoesNotDuplicateAssets(t *testing.T) {
	records := &resultMemory{readable: true, candidate: metadatascrapemodel.CandidateIdentity{ID: "existing"}}
	blobs := &responseBlobs{t: t, records: records}
	recorder := NewRecorder(records, blobs, func() time.Time { return time.UnixMilli(100) })
	created, err := recorder.Record(t.Context(), metadatascrapemodel.LookupAttempt{
		Claim: metadatascrapemodel.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 3, AllowCandidate: true,
		Lookup: metadatascrapemodel.ResolvedLookup{CachedResponseID: "cached", Result: metadatamodel.LookupResult{
			Outcome: metadatamodel.OutcomeHit, RawResponse: []byte("raw"),
			Candidate: &metadatamodel.Candidate{ProviderGameID: "provider-game", Metadata: json.RawMessage(`{"title":"title"}`), Assets: []metadatamodel.AssetReference{{ProviderAssetID: "asset"}}},
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
	_, err := recorder.Record(t.Context(), metadatascrapemodel.LookupAttempt{AllowCandidate: true, Lookup: metadatascrapemodel.ResolvedLookup{Result: metadatamodel.LookupResult{
		Candidate: &metadatamodel.Candidate{Metadata: json.RawMessage(`{"invalid":NaN}`)},
	}}})
	var invalid *json.SyntaxError
	if !errors.As(err, &invalid) || records.calls != 0 {
		t.Fatalf("invalid candidate persisted: calls=%d error=%v", records.calls, err)
	}
}

func TestResultCommitFailureDoesNotReportCreatedCandidate(t *testing.T) {
	records := &resultMemory{readable: true, candidate: metadatascrapemodel.CandidateIdentity{Created: true}, lateError: context.DeadlineExceeded}
	recorder := NewRecorder(records, &responseBlobs{t: t, records: records}, time.Now)
	created, err := recorder.Record(t.Context(), metadatascrapemodel.LookupAttempt{
		Claim: metadatascrapemodel.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 1, AllowCandidate: true,
		Lookup: metadatascrapemodel.ResolvedLookup{CachedResponseID: "cached", Result: metadatamodel.LookupResult{Candidate: &metadatamodel.Candidate{ProviderGameID: "game"}}},
	})
	if created || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failed commit returned success: %t / %v", created, err)
	}
	if records.hit.CandidateID == "" {
		t.Fatal("late failure did not execute candidate writes")
	}
}

func (memory *resultMemory) Subject(context.Context, string) (metadatascrapemodel.Subject, error) {
	return metadatascrapemodel.Subject{Kind: "IMPORT_ITEM", ID: "item"}, nil
}

func (memory *resultMemory) Enqueue(context.Context, metadatascrapemodel.MediaJobPlan) error {
	return nil
}
