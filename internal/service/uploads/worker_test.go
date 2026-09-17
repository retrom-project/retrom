package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/uploads"
)

func TestStaleFinalizationCannotWriteNewRound(t *testing.T) {
	jobID := "job"
	for _, test := range []struct {
		name             string
		round, execution int64
	}{
		{"old round", 2, 1},
		{"old execution", 1, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &workerRepository{
				current: model.SessionState{ID: "upload", State: "FINALIZING", FinalizationNo: test.round, FinalizeJobID: &jobID},
				job:     model.Job{ID: jobID, State: "RUNNING", ExecutionNo: test.execution},
			}
			service := New(repository, nil, "", time.Now)
			called := false
			stopped, err := service.finalizeWrite(t.Context(), model.Run{UploadID: "upload", JobID: jobID, FinalizationNo: 1, ExecutionNo: 1},
				func(model.WriteScope, model.SessionState) error { called = true; return nil })
			if err != nil || !stopped || called {
				t.Fatalf("stale worker continued: stopped=%v called=%v error=%v", stopped, called, err)
			}
		})
	}
}

func TestFinalizeClaimFailurePreservesCause(t *testing.T) {
	failure := errors.New("storage unavailable")
	created, err := prepareFinalization("upload", 1, finalizationTestNow().UnixMilli(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repository := &workerRepository{
		current: model.SessionState{ID: "upload", State: "FINALIZING", FinalizationNo: 1, FinalizeJobID: &created.Run.JobID},
		job: model.Job{
			ID: created.Run.JobID, State: "QUEUED", ExecutionNo: 1, Kind: "UPLOAD_FINALIZE", Scope: "UPLOAD_SESSION", ScopeID: "upload",
			Input: string(created.InputJSON), InputDigest: created.InputDigest, MaxAttempts: 2,
		}, claimError: failure,
	}
	err = New(repository, nil, "", finalizationTestNow).Run(t.Context(), created.Run.JobID)
	if !errors.Is(err, failure) {
		t.Fatalf("claim failure lost or finalizer continued: %v", err)
	}
}

func finalizationTestNow() time.Time { return time.Date(2028, 3, 4, 5, 6, 7, 0, time.UTC) }

func TestPartReaderChecksActualBytesAndCancellation(t *testing.T) {
	sum := sha256.Sum256([]byte("bytes"))
	for _, data := range []string{"bytes", "shorter", "byte", "wrong"} {
		reader := &partReader{
			ctx: t.Context(), reader: strings.NewReader(data), hash: sha256.New(),
			expected: model.Part{Size: 5, SHA256: hex.EncodeToString(sum[:])},
		}
		_, err := io.ReadAll(reader)
		if (err == nil) != (data == "bytes") || err != nil && !errors.Is(err, errPartCorrupt) {
			t.Fatalf("part validation for %q: %v", data, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := &partReader{ctx: ctx, reader: strings.NewReader("bytes"), hash: sha256.New()}
	if _, err := io.ReadAll(reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was ignored: %v", err)
	}
}

type workerRepository struct {
	model.Repository
	current    model.SessionState
	job        model.Job
	claimError error
}

func (repository *workerRepository) CommitWrite(_ context.Context, work func(model.WriteScope) error) error {
	return work(model.WriteScope{Sessions: workerSessions{current: repository.current}, Jobs: workerJobs{repository: repository}, Finalize: workerFinalize{}})
}

type workerSessions struct {
	model.SessionRecords
	current model.SessionState
}

func (records workerSessions) Current(context.Context, string) (model.SessionState, error) {
	return records.current, nil
}

type workerJobs struct {
	model.JobRecords
	repository *workerRepository
}

func (records workerJobs) Get(context.Context, string) (model.Job, error) {
	return records.repository.job, nil
}

func (records workerJobs) Claim(context.Context, model.JobClaim) (bool, error) {
	return false, records.repository.claimError
}

func TestPartReceivePreservesReadFailure(t *testing.T) {
	failure := errors.New("request stream interrupted")
	service := New(nil, nil, t.TempDir(), time.Now)
	_, err := service.stageUploadPart("upload", "file", 0, byteRange{start: 0, end: 4, total: 5},
		strings.Repeat("a", 64), failedPartBody{failure: failure})
	if !errors.Is(err, failure) || !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("receive failure lost: %v", err)
	}
}

type failedPartBody struct{ failure error }

func (body failedPartBody) Read([]byte) (int, error) { return 0, body.failure }

type workerFinalize struct{ model.FinalizationRecords }

func (workerFinalize) Manifest(context.Context, string) ([]model.FrozenFile, error) { return nil, nil }
