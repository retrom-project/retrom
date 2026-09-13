package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type memoryJobs struct {
	Repository
	job                Job
	input              []byte
	readErr            error
	cancellation       *Cancellation
	retry              *RetryWrite
	importCancellation bool
	opened             int
}

func (repository *memoryJobs) WithWrite(_ context.Context, work func(Records) error) error {
	repository.opened++
	return work(repository)
}

func (repository *memoryJobs) Get(context.Context, string) (Job, error) {
	return repository.job, repository.readErr
}

func (repository *memoryJobs) Input(context.Context, string, int64) ([]byte, error) {
	return repository.input, nil
}

func (repository *memoryJobs) Cancel(_ context.Context, change Cancellation) error {
	repository.cancellation = &change
	return nil
}

func (repository *memoryJobs) Retry(_ context.Context, change RetryWrite) error {
	repository.retry = &change
	return nil
}

func (repository *memoryJobs) CancelServerImport(context.Context, Cancellation) error {
	repository.importCancellation = true
	return nil
}

func TestCancelEligibilityPrecedesWrites(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"SUCCEEDED", "CANCELLED", "CANCEL_REQUESTED", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			repository := &memoryJobs{job: Job{State: state, Cancellable: true, Version: 2}}
			_, _, err := New(repository, time.Now).Cancel(t.Context(), "job", 2, "cancel")
			if !errors.Is(err, ErrConflict) || repository.cancellation != nil {
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
		if !errors.Is(err, ErrConflict) || repository.opened != 0 {
			t.Fatalf("error=%v transactions=%d", err, repository.opened)
		}
	}
}

func TestRunningServerImportCancellationRemainsPending(t *testing.T) {
	t.Parallel()
	repository := &memoryJobs{job: Job{Kind: "SERVER_BIOS_IMPORT", State: "RUNNING", Cancellable: true, Version: 2, ExecutionNo: 3}}
	result, pending, err := New(repository, func() time.Time { return time.UnixMilli(1234) }).Cancel(t.Context(), "job", 2, " stop ")
	if err != nil || !pending || result.State != "CANCEL_REQUESTED" || result.Version != 3 || result.ExecutionNo != 3 {
		t.Fatalf("result=%+v pending=%v error=%v", result, pending, err)
	}
	change := repository.cancellation
	if change == nil || change.FinishedAtMS != nil || change.Reason != "stop" || change.AtMS != 1234 || !repository.importCancellation {
		t.Fatalf("cancellation=%+v import=%v", change, repository.importCancellation)
	}
}

func TestRetryDomainJobsCannotUseGenericAction(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"METADATA_SCRAPE", "SERVER_BIOS_IMPORT", "REVIEW_BULK_APPROVE"} {
		t.Run(kind, func(t *testing.T) {
			repository := &memoryJobs{job: Job{Kind: kind, State: "FAILED", Retryable: true, Version: 4}}
			_, err := New(repository, time.Now).Retry(t.Context(), "job", 4)
			if !errors.Is(err, ErrRetryViaDomain) || repository.retry != nil {
				t.Fatalf("error=%v retry=%+v", err, repository.retry)
			}
		})
	}
}

func TestRetryRejectsMismatchedInputBeforeWrites(t *testing.T) {
	t.Parallel()
	repository := &memoryJobs{
		job:   Job{Kind: "MEDIA_FETCH", ScopeType: "GAME", ScopeID: "game", State: "FAILED", Retryable: true, Version: 1},
		input: []byte(`{"schemaVersion":1,"kind":"MEDIA_FETCH","scope":{"type":"GAME","id":"other"},"executionId":"00000000-0000-7000-8000-000000000001","inputs":{}}`),
	}
	_, err := New(repository, time.Now).Retry(t.Context(), "job", 1)
	if !errors.Is(err, ErrConflict) || repository.retry != nil {
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
