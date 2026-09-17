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
			_, stopped, err := repository.CommitReadCandidates(t.Context(), model.FinalizationOwnershipCommand{
				Run:   model.Run{UploadID: "upload", JobID: jobID, FinalizationNo: 1, ExecutionNo: 1},
				NowMS: time.Now().UnixMilli(),
			})
			if err != nil || !stopped {
				t.Fatalf("stale worker continued: stopped=%v error=%v", stopped, err)
			}
		})
	}
}

func TestFinalizeClaimFailurePreservesCause(t *testing.T) {
	failure := errors.New("storage unavailable")
	repository := &workerRepository{
		current: model.SessionState{ID: "upload", State: "FINALIZING", FinalizationNo: 1, FinalizeJobID: strPtr("job-1")},
		job: model.Job{
			ID: "job-1", State: "QUEUED", ExecutionNo: 1, Kind: "UPLOAD_FINALIZE", Scope: "UPLOAD_SESSION", ScopeID: "upload",
			MaxAttempts: 2,
		}, claimError: failure,
	}
	_, err := repository.CommitClaimFinalization(t.Context(), model.ClaimFinalizationCommand{
		JobID: "job-1", WorkerID: "w", NowMS: finalizationTestNow().UnixMilli(),
	})
	if !errors.Is(err, failure) {
		t.Fatalf("claim failure lost: %v", err)
	}
}

func strPtr(s string) *string        { return &s }
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
	current    model.SessionState
	job        model.Job
	claimError error
}

func (*workerRepository) Snapshot(context.Context, string) (model.Session, error) { panic("not used") }

func (*workerRepository) Target(context.Context, model.FileKey) (model.PartTarget, error) {
	panic("not used")
}
func (*workerRepository) Parts(context.Context, string) ([]model.Part, error)  { panic("not used") }
func (*workerRepository) Recoverable(context.Context, int64) ([]string, error) { panic("not used") }
func (*workerRepository) CommitCreateSession(context.Context, model.Registration) error {
	panic("not used")
}

func (*workerRepository) CommitRecordPart(context.Context, model.RecordPartCommand) error {
	panic("not used")
}

func (*workerRepository) CommitRepairPart(context.Context, model.FileKey, int) error {
	panic("not used")
}

func (*workerRepository) CommitComplete(context.Context, model.CompleteCommand) (model.Run, error) {
	panic("not used")
}

func (*workerRepository) CommitCancel(context.Context, model.CancelCommand) (model.CancelResult, error) {
	panic("not used")
}

func (r *workerRepository) CommitClaimFinalization(_ context.Context, cmd model.ClaimFinalizationCommand) (model.ClaimResult, error) {
	if r.claimError != nil {
		return model.ClaimResult{}, r.claimError
	}
	return model.ClaimResult{Acquired: true, Run: model.Run{
		UploadID: r.current.ID, JobID: cmd.JobID,
	}}, nil
}

func (*workerRepository) CommitObserveFinalization(context.Context, model.ObserveCommand) error {
	return nil
}

func (r *workerRepository) CommitReadCandidates(_ context.Context, cmd model.FinalizationOwnershipCommand) ([]model.Candidate, bool, error) {
	if r.current.FinalizationNo != cmd.Run.FinalizationNo {
		return nil, true, nil
	}
	if r.job.ExecutionNo != cmd.Run.ExecutionNo {
		return nil, true, nil
	}
	return nil, false, nil
}

func (*workerRepository) CommitPublishFile(context.Context, model.PublishFileCommand) (bool, error) {
	panic("not used")
}

func (*workerRepository) CommitFinishFinalization(context.Context, model.FinishFinalizationCommand) (bool, error) {
	return false, nil
}

func (*workerRepository) CommitFinalizationFailure(context.Context, model.FinalizationFailureCommand) (bool, error) {
	return false, nil
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
