package uploads

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCancelledQueuedJobFinishesMatchingUpload(t *testing.T) {
	for _, currentExecution := range []int64{1, 2} {
		repository, run := terminationFixture()
		repository.job.State = "CANCELLED"
		repository.job.ExecutionNo = currentExecution
		claimed, err := New(repository, nil, t.TempDir(), time.Now).claimFinalization(t.Context(), run)
		if err != nil || claimed {
			t.Fatalf("cancelled claim: claimed=%v error=%v", claimed, err)
		}
		want := ""
		if currentExecution == run.ExecutionNo {
			want = "CANCELLED"
		}
		if repository.sessionFinish.State != want || (repository.fileFailure.Code != "") != (want != "") {
			t.Fatalf("execution %d: session=%+v files=%+v", currentExecution, repository.sessionFinish, repository.fileFailure)
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
	Repository
	current       SessionState
	job           Job
	sessionFinish SessionFinish
	jobFinish     JobFinish
	fileFailure   PendingFailure
	bounded       bool
}

func terminationFixture() (*terminationRepository, Run) {
	run := Run{UploadID: "upload", JobID: "job", FinalizationNo: 1, ExecutionNo: 1}
	return &terminationRepository{
		current: SessionState{
			ID: run.UploadID, State: "FINALIZING", Version: 1,
			FinalizeJobID: &run.JobID, FinalizationNo: 1,
		},
		job: Job{ID: run.JobID, State: "RUNNING", ExecutionNo: 1},
	}, run
}

func (repository *terminationRepository) WithWrite(ctx context.Context, work func(WriteScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	repository.bounded = ok && time.Until(deadline) <= 5*time.Second
	return work(WriteScope{
		Sessions: terminationSessions{repository: repository},
		Jobs:     terminationJobs{repository: repository}, Files: terminationFiles{repository: repository},
	})
}

type terminationSessions struct {
	SessionRecords
	repository *terminationRepository
}

func (records terminationSessions) Current(context.Context, string) (SessionState, error) {
	return records.repository.current, nil
}

func (records terminationSessions) Finish(_ context.Context, finish SessionFinish) error {
	records.repository.sessionFinish = finish
	return nil
}

type terminationJobs struct {
	JobRecords
	repository *terminationRepository
}

func (records terminationJobs) Get(context.Context, string) (Job, error) {
	return records.repository.job, nil
}

func (records terminationJobs) Finish(_ context.Context, finish JobFinish) error {
	if finish.ExpectedState != records.repository.job.State {
		return errors.New("unexpected job state")
	}
	records.repository.jobFinish = finish
	return nil
}

type terminationFiles struct {
	FileRecords
	repository *terminationRepository
}

func (records terminationFiles) FailPending(_ context.Context, failure PendingFailure) error {
	records.repository.fileFailure = failure
	return nil
}
