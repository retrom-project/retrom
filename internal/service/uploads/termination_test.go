package uploads

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/uploads"
)

func TestCancelledQueuedJobFinishesMatchingUpload(t *testing.T) {
	for _, currentRound := range []int64{1, 2} {
		repository, run := terminationFixture()
		repository.claimCancelled = currentRound == run.FinalizationNo
		repository.current.FinalizationNo = currentRound
		claim, err := New(repository, nil, t.TempDir(), finalizationTestNow).claim(t.Context(), run.JobID)
		if err != nil && currentRound == run.FinalizationNo || claim.Acquired {
			t.Fatalf("cancelled claim: acquired=%v error=%v", claim.Acquired, err)
		}
		want := ""
		if currentRound == run.FinalizationNo {
			want = "CANCELLED"
		}
		if repository.sessionFinishState != want || (repository.fileFailureCode != "") != (want != "") {
			t.Fatalf("execution %d: session=%s files=%s", currentRound, repository.sessionFinishState, repository.fileFailureCode)
		}
	}
}

func TestUploadCancellationAcceptsAlreadyCancelledJob(t *testing.T) {
	repository, _ := terminationFixture()
	repository.cancelResult = model.CancelResult{
		Result:  model.Canceled{UploadID: "upload", State: "CANCELLED", Version: 2},
		Pending: false,
	}
	repository.sessionFinishState = "CANCELLED"
	result, pending, err := New(repository, nil, t.TempDir(), time.Now).Cancel(t.Context(), "upload", 1)
	if err != nil || pending || result.State != "CANCELLED" || repository.sessionFinishState != "CANCELLED" {
		t.Fatalf("cancelled job synchronization: result=%+v pending=%v error=%v", result, pending, err)
	}
}

func TestTimedOutFinalizationPersistsFailureWithBoundedContext(t *testing.T) {
	repository, run := terminationFixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := New(repository, nil, t.TempDir(), time.Now).fail(ctx, run, context.DeadlineExceeded)
	if err != nil || repository.failureState != "FAILED" {
		t.Fatalf("timeout stranded upload: failure=%s error=%v", repository.failureState, err)
	}
	if !repository.bounded {
		t.Fatal("failure persistence has no deadline")
	}
}

type terminationRepository struct {
	current            model.SessionState
	job                model.Job
	sessionFinishState string
	fileFailureCode    string
	failureState       string
	bounded            bool
	claimCancelled     bool
	cancelResult       model.CancelResult
}

func terminationFixture() (*terminationRepository, model.Run) {
	run := model.Run{UploadID: "upload", JobID: "job", FinalizationNo: 1, ExecutionNo: 1}
	return &terminationRepository{
		current: model.SessionState{
			ID: run.UploadID, State: "FINALIZING", Version: 1,
			FinalizeJobID: &run.JobID, FinalizationNo: 1,
		},
		job: model.Job{
			ID: run.JobID, State: "RUNNING", ExecutionNo: 1, Kind: "UPLOAD_FINALIZE",
			Scope: "UPLOAD_SESSION", ScopeID: run.UploadID,
		},
	}, run
}

func (*terminationRepository) Snapshot(context.Context, string) (model.Session, error) {
	panic("not used")
}

func (*terminationRepository) Target(context.Context, model.FileKey) (model.PartTarget, error) {
	panic("not used")
}

func (*terminationRepository) Parts(context.Context, string) ([]model.Part, error) { panic("not used") }

func (*terminationRepository) Recoverable(context.Context, int64) ([]string, error) {
	panic("not used")
}

func (*terminationRepository) CommitCreateSession(context.Context, model.Registration) error {
	panic("not used")
}

func (*terminationRepository) CommitRecordPart(context.Context, model.RecordPartCommand) error {
	panic("not used")
}

func (*terminationRepository) CommitRepairPart(context.Context, model.FileKey, int) error {
	panic("not used")
}

func (*terminationRepository) CommitComplete(context.Context, model.CompleteCommand) (model.Run, error) {
	panic("not used")
}

func (r *terminationRepository) CommitCancel(_ context.Context, _ model.CancelCommand) (model.CancelResult, error) {
	return r.cancelResult, nil
}

func (r *terminationRepository) CommitClaimFinalization(_ context.Context, cmd model.ClaimFinalizationCommand) (model.ClaimResult, error) {
	if r.claimCancelled {
		r.sessionFinishState = "CANCELLED"
		r.fileFailureCode = "UPLOAD_CANCELLED"
		return model.ClaimResult{Cancelled: true, Run: model.Run{UploadID: r.current.ID, JobID: cmd.JobID}}, nil
	}
	return model.ClaimResult{}, nil
}

func (*terminationRepository) CommitObserveFinalization(context.Context, model.ObserveCommand) error {
	return nil
}

func (*terminationRepository) CommitReadCandidates(context.Context, model.FinalizationOwnershipCommand) ([]model.Candidate, bool, error) {
	return nil, false, nil
}

func (*terminationRepository) CommitPublishFile(context.Context, model.PublishFileCommand) (bool, error) {
	panic("not used")
}

func (*terminationRepository) CommitFinishFinalization(context.Context, model.FinishFinalizationCommand) (bool, error) {
	return false, nil
}

func (r *terminationRepository) CommitFinalizationFailure(ctx context.Context, cmd model.FinalizationFailureCommand) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deadline, ok := ctx.Deadline()
	r.bounded = ok && time.Until(deadline) <= 5*time.Second
	r.failureState = "FAILED"
	if errors.Is(cmd.Cause, context.Canceled) {
		r.failureState = "CANCELLED"
	}
	return r.failureState == "CANCELLED", nil
}
