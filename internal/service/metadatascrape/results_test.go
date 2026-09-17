package metadatascrape

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	model "retrom/internal/model/metadatascrape"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
)

type resultMemory struct {
	inTransaction bool
	calls         int
	lateError     error
	readable      bool
	created       bool
	attempt       model.RecordCommand
}

func (memory *resultMemory) CommitRecord(_ context.Context, cmd model.RecordCommand) (model.RecordResult, error) {
	memory.calls++
	memory.inTransaction = true
	defer func() { memory.inTransaction = false }()
	memory.attempt = cmd
	if !memory.readable {
		return model.RecordResult{}, model.ErrExecutionLost
	}
	if memory.lateError != nil {
		return model.RecordResult{}, memory.lateError
	}
	return model.RecordResult{Created: memory.created}, nil
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
	if created || blobs.calls != 1 || records.calls != 1 {
		t.Fatalf("response recording: calls=%d blobs=%d created=%t", records.calls, blobs.calls, created)
	}
	cmd := records.attempt
	if cmd.Blob == nil || cmd.Blob.SHA256 != "raw" || cmd.Candidate != nil {
		t.Fatalf("command: blob=%v candidate=%v", cmd.Blob, cmd.Candidate)
	}
}

func TestCachedResponseSkipsBlobPreparation(t *testing.T) {
	records := &resultMemory{readable: true}
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
	if created || blobs.calls != 0 || records.calls != 1 {
		t.Fatalf("cached result: blobs=%d calls=%d created=%t", blobs.calls, records.calls, created)
	}
	cmd := records.attempt
	if cmd.Blob != nil || cmd.Candidate == nil || cmd.Candidate.Value.ProviderGameID != "provider-game" {
		t.Fatalf("cached command: blob=%v candidate=%v", cmd.Blob, cmd.Candidate)
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
	records := &resultMemory{readable: true, lateError: context.DeadlineExceeded}
	recorder := NewRecorder(records, &responseBlobs{t: t, records: records}, time.Now)
	created, err := recorder.Record(t.Context(), model.LookupAttempt{
		Claim: model.WorkerClaim{RunID: "run"}, EvidenceID: "evidence", AttemptNo: 1, AllowCandidate: true,
		Lookup: model.ResolvedLookup{CachedResponseID: "cached", Result: hasheous.LookupResult{Candidate: &hasheous.Candidate{ProviderGameID: "game"}}},
	})
	if created || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failed commit returned success: %t / %v", created, err)
	}
}
