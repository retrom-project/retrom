package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/jobs"
)

type dispatchRepository struct {
	*memoryJobs
	active    bool
	completed bool
	commitErr error
}

func (repository *dispatchRepository) WithWrite(ctx context.Context, work func(model.Records) error) error {
	repository.active = true
	defer func() { repository.active = false }()
	if err := repository.memoryJobs.WithWrite(ctx, work); err != nil {
		return err
	}
	repository.completed = true
	return repository.commitErr
}

type dispatchHandler struct {
	repository *dispatchRepository
	command    DomainCancellation
	called     bool
	failure    error
	context    context.Context
}

func (handler *dispatchHandler) CancelJob(ctx context.Context, command DomainCancellation) (Result, bool, error) {
	if handler.repository.active || !handler.repository.completed {
		return Result{}, false, errors.New("dispatch before scope closed")
	}
	handler.called = true
	handler.command = command
	handler.context = ctx
	return Result{JobID: command.JobID, State: "CANCEL_REQUESTED", ExecutionNo: 7, Version: command.ExpectedVersion + 1}, true, handler.failure
}

func dispatchFixture() (*dispatchRepository, *dispatchHandler) {
	repository := &dispatchRepository{memoryJobs: &memoryJobs{job: model.Job{Kind: "SERVER_EMULATIONSTATION_SCAN", ScopeType: "EMULATIONSTATION_IMPORT", ScopeID: "plan", State: "RUNNING", Cancellable: true, Version: 4, ExecutionNo: 7}}}
	return repository, &dispatchHandler{repository: repository}
}

func TestDomainCancellationClosesUnmodifiedScopeBeforeDispatch(t *testing.T) {
	t.Parallel()
	repository, handler := dispatchFixture()
	ctx := t.Context()
	result, pending, err := New(repository, time.Now).WithDomainCancellation(map[string]DomainCanceller{"SERVER_EMULATIONSTATION_SCAN": handler}).Cancel(ctx, "scan-job", 4, " stop ")
	want := DomainCancellation{JobID: "scan-job", Kind: "SERVER_EMULATIONSTATION_SCAN", ScopeID: "plan", Reason: "stop", ExpectedVersion: 4}
	if err != nil || !pending || result.Version != 5 || result.ExecutionNo != 7 || handler.command != want || handler.context != ctx || repository.cancellation != nil {
		t.Fatalf("result=%#v pending=%v error=%v command=%#v", result, pending, err, handler.command)
	}
}

func TestDomainCancellationDropsPartialFailureResponse(t *testing.T) {
	t.Parallel()
	repository, handler := dispatchFixture()
	handler.failure = errors.New("domain storage unavailable")
	result, pending, err := New(repository, time.Now).WithDomainCancellation(map[string]DomainCanceller{"SERVER_EMULATIONSTATION_SCAN": handler}).Cancel(t.Context(), "scan-job", 4, "stop")
	if !errors.Is(err, handler.failure) || pending || result.JobID != "" {
		t.Fatalf("result=%#v pending=%v error=%v", result, pending, err)
	}
}

func TestDomainCancellationNeverDispatchesFailedOuterCommit(t *testing.T) {
	t.Parallel()
	repository, handler := dispatchFixture()
	repository.commitErr = errors.New("outer commit unavailable")
	result, pending, err := New(repository, time.Now).WithDomainCancellation(map[string]DomainCanceller{"SERVER_EMULATIONSTATION_SCAN": handler}).Cancel(t.Context(), "scan-job", 4, "stop")
	if !errors.Is(err, repository.commitErr) || handler.called || pending || result.JobID != "" {
		t.Fatalf("result=%#v pending=%v error=%v called=%v", result, pending, err, handler.called)
	}
}

func TestDomainCancellationRegistryCopiesAndMerges(t *testing.T) {
	t.Parallel()
	repository, handler := dispatchFixture()
	registry := map[string]DomainCanceller{"SERVER_EMULATIONSTATION_SCAN": handler}
	original := New(repository, time.Now)
	configured := original.WithDomainCancellation(registry)
	delete(registry, "SERVER_EMULATIONSTATION_SCAN")
	merged := configured.WithDomainCancellation(map[string]DomainCanceller{"SERVER_PEGASUS_SCAN": handler})
	if len(original.cancellations) != 0 || len(configured.cancellations) != 1 || len(merged.cancellations) != 2 {
		t.Fatal("configuration mutated an existing registry")
	}
	if _, _, err := merged.Cancel(t.Context(), "scan-job", 4, "stop"); err != nil || !handler.called {
		t.Fatalf("copied registration error=%v called=%v", err, handler.called)
	}
}

func TestDomainCancellationPreconditionsNeverCallHandler(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"version", "state", "disabled", "reason", "read"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			repository, handler := dispatchFixture()
			version := int64(4)
			reason := "stop"
			want := model.ErrConflict
			switch scenario {
			case "version":
				version++
			case "state":
				repository.job.State = "SUCCEEDED"
			case "disabled":
				repository.job.Cancellable = false
			case "reason":
				reason = " "
			case "read":
				repository.readErr = errors.New("read unavailable")
				want = repository.readErr
			}
			result, pending, err := New(repository, time.Now).WithDomainCancellation(map[string]DomainCanceller{"SERVER_EMULATIONSTATION_SCAN": handler}).Cancel(t.Context(), "scan-job", version, reason)
			if !errors.Is(err, want) || handler.called || pending || result.JobID != "" || repository.cancellation != nil {
				t.Fatalf("result=%#v pending=%v error=%v called=%v", result, pending, err, handler.called)
			}
		})
	}
}
