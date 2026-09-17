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
		repository.job.State = "CANCELLED"
		repository.current.FinalizationNo = currentRound
		claim, err := New(repository, nil, t.TempDir(), finalizationTestNow).claim(t.Context(), run.JobID)
		claimed := claim.Acquired
		if err != nil && currentRound == run.FinalizationNo || claimed {
			t.Fatalf("cancelled claim: claimed=%v error=%v", claimed, err)
		}
		want := ""
		if currentRound == run.FinalizationNo {
			want = "CANCELLED"
		}
		if repository.sessionFinish.State != want || (repository.fileFailure.Code != "") != (want != "") {
			t.Fatalf("execution %d: session=%+v files=%+v", currentRound, repository.sessionFinish, repository.fileFailure)
		}
	}
}

func TestUploadCancellationAcceptsAlreadyCancelledJob(t *testing.T) {
	repository, _ := terminationFixture()
	repository.job.State = "CANCELLED"
	result, pending, err := New(repository, nil, t.TempDir(), time.Now).Cancel(t.Context(), "upload", 1)
	if err != nil || pending || result.State != "CANCELLED" || repository.sessionFinish.State != "CANCELLED" {
		t.Fatalf("cancelled job synchronization: result=%+v pending=%v error=%v", result, pending, err)
	}
}

func TestTimedOutFinalizationPersistsFailureWithBoundedContext(t *testing.T) {
	repository, run := terminationFixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := New(repository, nil, t.TempDir(), time.Now).fail(ctx, run, context.DeadlineExceeded)
	if err != nil || repository.sessionFinish.State != "FAILED" || repository.jobFinish.State != "FAILED" {
		t.Fatalf("timeout stranded upload: session=%+v job=%+v error=%v", repository.sessionFinish, repository.jobFinish, err)
	}
	if !repository.bounded {
		t.Fatal("failure persistence has no deadline")
	}
}

type terminationRepository struct {
	model.Repository
	current       model.SessionState
	job           model.Job
	sessionFinish model.SessionFinish
	jobFinish     model.JobFinish
	fileFailure   model.PendingFailure
	bounded       bool
}

func terminationFixture() (*terminationRepository, model.Run) {
	run := model.Run{UploadID: "upload", JobID: "job", FinalizationNo: 1, ExecutionNo: 1}
	return &terminationRepository{
		current: model.SessionState{
			ID: run.UploadID, State: "FINALIZING", Version: 1,
			FinalizeJobID: &run.JobID, FinalizationNo: 1,
		},
		job: terminationJob(run),
	}, run
}

func (repository *terminationRepository) CommitWrite(ctx context.Context, work func(model.WriteScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	repository.bounded = ok && time.Until(deadline) <= 5*time.Second
	return work(model.WriteScope{
		Sessions: terminationSessions{repository: repository},
		Jobs:     terminationJobs{repository: repository}, Files: terminationFiles{repository: repository},
	})
}

type terminationSessions struct {
	model.SessionRecords
	repository *terminationRepository
}

func (records terminationSessions) Current(context.Context, string) (model.SessionState, error) {
	return records.repository.current, nil
}

func (records terminationSessions) Finish(_ context.Context, finish model.SessionFinish) error {
	records.repository.sessionFinish = finish
	return nil
}

type terminationJobs struct {
	model.JobRecords
	repository *terminationRepository
}

func (records terminationJobs) Get(context.Context, string) (model.Job, error) {
	return records.repository.job, nil
}

func (records terminationJobs) Finish(_ context.Context, finish model.JobFinish) error {
	if finish.ExpectedState != records.repository.job.State {
		return errors.New("unexpected job state")
	}
	records.repository.jobFinish = finish
	return nil
}

type terminationFiles struct {
	model.FileRecords
	repository *terminationRepository
}

func (records terminationFiles) FailPending(_ context.Context, failure model.PendingFailure) error {
	records.repository.fileFailure = failure
	return nil
}

func terminationJob(run model.Run) model.Job {
	created, _ := prepareFinalization(run.UploadID, run.FinalizationNo, finalizationTestNow().UnixMilli(), nil)
	return model.Job{
		ID: run.JobID, State: "RUNNING", ExecutionNo: 1, Kind: "UPLOAD_FINALIZE", Scope: "UPLOAD_SESSION", ScopeID: run.UploadID,
		Input: string(created.InputJSON), InputDigest: created.InputDigest,
	}
}
