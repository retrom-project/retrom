package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/jobs"
)

type memoryJobs struct {
	model.Repository
	job                model.Job
	input              []byte
	readErr            error
	cancellation       *model.Cancellation
	retry              *model.RetryWrite
	importCancellation bool
	opened             int
}

func (repository *memoryJobs) WithRead(_ context.Context, _ func(model.ReadRecords) error) error {
	return nil
}

func (repository *memoryJobs) CommitCancel(
	_ context.Context, cmd model.CancelCommand,
) (model.CancelResult, error) {
	repository.opened++
	if repository.readErr != nil {
		return model.CancelResult{}, repository.readErr
	}
	job := repository.job
	if job.Version != cmd.ExpectedVersion || !job.Cancellable ||
		!model.CancellableJobState(job.State, job.Retryable) {
		return model.CancelResult{}, model.ErrConflict
	}
	if job.Kind == "REVIEW_BULK_APPROVE" {
		return model.CancelResult{}, model.ErrRetryViaDomain
	}
	for _, k := range cmd.DomainHandlerFor {
		if k == job.Kind {
			return model.CancelResult{NeedsDomain: true, DomainJob: job}, nil
		}
	}
	state := "CANCELLED"
	var finishedAtMS *int64
	pending := job.State == "RUNNING"
	if pending {
		state = "CANCEL_REQUESTED"
	} else {
		finishedAtMS = &cmd.NowMS
	}
	event, _ := json.Marshal(struct {
		Reason string `json:"reason"`
	}{Reason: cmd.Reason})
	change := model.Cancellation{
		JobID: cmd.JobID, ExpectedVersion: cmd.ExpectedVersion,
		State: state, Reason: cmd.Reason,
		AtMS: cmd.NowMS, FinishedAtMS: finishedAtMS,
		Event: event,
	}
	repository.cancellation = &change
	if job.Kind == "SERVER_BIOS_IMPORT" {
		repository.importCancellation = true
	}
	return model.CancelResult{
		Result: model.Result{
			Kind: job.Kind, JobID: cmd.JobID,
			State: state, ExecutionNo: job.ExecutionNo,
			Version: job.Version + 1,
		},
		Pending: pending,
	}, nil
}

func (repository *memoryJobs) CommitRetry(
	_ context.Context, cmd model.RetryCommand,
) (model.Result, error) {
	repository.opened++
	if repository.readErr != nil {
		return model.Result{}, repository.readErr
	}
	job := repository.job
	if err := model.RetryEligibility(job, cmd.ExpectedVersion); err != nil {
		return model.Result{}, err
	}
	change, result, err := model.BuildRetryWrite(
		cmd.JobID, cmd.ExpectedVersion, repository.input, job, cmd.NowMS,
	)
	if err != nil {
		return model.Result{}, err
	}
	repository.retry = &change
	return result, nil
}

func TestCancelEligibilityPrecedesWrites(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"SUCCEEDED", "CANCELLED", "CANCEL_REQUESTED", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			repository := &memoryJobs{job: model.Job{State: state, Cancellable: true, Version: 2}}
			_, _, err := New(repository, time.Now).Cancel(t.Context(), "job", 2, "cancel")
			if !errors.Is(err, model.ErrConflict) || repository.cancellation != nil {
				t.Fatalf("error=%v cancellation=%+v", err, repository.cancellation)
			}
		})
	}
}

func TestCancelRejectsInvalidReasonWithoutTransaction(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{" ", strings.Repeat("字", 501)} {
		repository := &memoryJobs{}
		_, _, err := New(repository, time.Now).Cancel(t.Context(), "job", 1, reason)
		if !errors.Is(err, model.ErrConflict) || repository.opened != 0 {
			t.Fatalf("error=%v transactions=%d", err, repository.opened)
		}
	}
}

func TestRunningServerImportCancellationRemainsPending(t *testing.T) {
	t.Parallel()
	repository := &memoryJobs{
		job: model.Job{
			Kind: "SERVER_BIOS_IMPORT", State: "RUNNING",
			Cancellable: true, Version: 2, ExecutionNo: 3,
		},
	}
	svc := New(repository, func() time.Time { return time.UnixMilli(1234) })
	result, pending, err := svc.Cancel(t.Context(), "job", 2, " stop ")
	if err != nil || !pending || result.State != "CANCEL_REQUESTED" ||
		result.Version != 3 || result.ExecutionNo != 3 {
		t.Fatalf("result=%+v pending=%v error=%v", result, pending, err)
	}
	change := repository.cancellation
	if change == nil || change.FinishedAtMS != nil || change.Reason != "stop" ||
		change.AtMS != 1234 || !repository.importCancellation {
		t.Fatalf("cancellation=%+v import=%v", change, repository.importCancellation)
	}
}

func TestRetryDomainJobsCannotUseGenericAction(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"METADATA_SCRAPE", "SERVER_BIOS_IMPORT", "REVIEW_BULK_APPROVE"} {
		t.Run(kind, func(t *testing.T) {
			repository := &memoryJobs{
				job: model.Job{Kind: kind, State: "FAILED", Retryable: true, Version: 4},
			}
			_, err := New(repository, time.Now).Retry(t.Context(), "job", 4)
			if !errors.Is(err, model.ErrRetryViaDomain) || repository.retry != nil {
				t.Fatalf("error=%v retry=%+v", err, repository.retry)
			}
		})
	}
}

func TestRetryRejectsMismatchedInputBeforeWrites(t *testing.T) {
	t.Parallel()
	repository := &memoryJobs{
		job: model.Job{
			Kind: "MEDIA_FETCH", ScopeType: "GAME", ScopeID: "game",
			State: "FAILED", Retryable: true, Version: 1,
		},
		input: []byte(`{"schemaVersion":1,"kind":"MEDIA_FETCH","scope":{"type":"GAME","id":"other"},"executionId":"00000000-0000-7000-8000-000000000001","inputs":{}}`),
	}
	_, err := New(repository, time.Now).Retry(t.Context(), "job", 1)
	if !errors.Is(err, model.ErrConflict) || repository.retry != nil {
		t.Fatalf("error=%v retry=%+v", err, repository.retry)
	}
}

func TestJobReadFailureRetainsCause(t *testing.T) {
	t.Parallel()
	readErr := errors.New("repository unavailable")
	repository := &memoryJobs{readErr: readErr}
	service := New(repository, time.Now)
	if _, _, err := service.Cancel(t.Context(), "job", 1, "stop"); !errors.Is(err, readErr) {
		t.Fatalf("cancel error=%v", err)
	}
	if _, err := service.Retry(t.Context(), "job", 1); !errors.Is(err, readErr) {
		t.Fatalf("retry error=%v", err)
	}
}
