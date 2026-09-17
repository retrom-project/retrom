package launch

import (
	"context"
	"errors"
	"reflect"
	model "retrom/internal/model/launch"
	"testing"
	"time"
)

func TestValidationWorkerCommitsOnceAndJoinsMonitor(t *testing.T) {
	worker, repository, ticker := newValidationTestWorker(t)
	if err := worker.Run(t.Context(), "job"); err != nil {
		t.Fatal(err)
	}
	if repository.work.State != "SUCCEEDED" || repository.work.Attempt != 1 || repository.variants != 1 || !reflect.DeepEqual(repository.events, []string{"STARTED", "SUCCEEDED"}) {
		t.Fatalf("unexpected result: %+v", repository)
	}
	if repository.terminal.Code != "READY" {
		t.Fatalf("success event lost READY code: %q", repository.terminal.Code)
	}
	if *repository.work.DeadlineMS-*repository.work.StartedMS != int64(30*time.Minute/time.Millisecond) {
		t.Fatal("execution budget changed")
	}
	select {
	case <-ticker.stopped:
	default:
		t.Fatal("monitor not joined")
	}
	if err := worker.Run(t.Context(), "job"); err != nil {
		t.Fatal(err)
	}
	if repository.work.Attempt != 1 || len(repository.events) != 2 {
		t.Fatal("duplicate run advanced execution")
	}
}

func TestValidationWorkerFailurePreservesCauseAndRollsBackTerminal(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	cause := errors.New("facts unavailable")
	eventCause := errors.New("event unavailable")
	repository.readFacts = func(context.Context) (model.ValidationFacts, error) { return model.ValidationFacts{}, cause }
	repository.finishError = eventCause
	err := worker.Run(t.Context(), "job")
	validationTestCause(t, err, cause)
	validationTestCause(t, err, eventCause)
	if repository.work.State != "RUNNING" || repository.variants != 0 || len(repository.events) != 1 {
		t.Fatal("partial terminal commit")
	}
}

func TestValidationWorkerChangedInputsCancelWithoutVariantWrites(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	repository.facts.Content.Source.GameVersion++
	validationTestCause(t, worker.Run(t.Context(), "job"), ErrValidationGameChanged)
	if repository.work.State != "CANCELLED" || repository.terminal.Code != "GAME_STATE_CHANGED" || repository.terminal.Retryable || repository.variants != 0 {
		t.Fatalf("unexpected cancellation: %+v", repository.terminal)
	}
}

func TestValidationWorkerFinalFactsRejectReplacementSelection(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	before := repository.facts
	repository.readFacts = func(context.Context) (model.ValidationFacts, error) {
		repository.facts.Content.Source.DependencySnapshot = "new native selection"
		return before, nil
	}
	validationTestCause(t, worker.Run(t.Context(), "job"), ErrValidationGameChanged)
	if repository.variants != 0 || repository.work.State != "CANCELLED" {
		t.Fatal("stale selection committed")
	}
}

func TestValidationWorkerMalformedInputIsTerminalAndNotRetryable(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	repository.work.SnapshotJSON = `{"schemaVersion":1}`
	validationTestCause(t, worker.Run(t.Context(), "job"), ErrValidationInput)
	if repository.work.State != "FAILED" || repository.terminal.Retryable || repository.variants != 0 {
		t.Fatal("malformed input not closed")
	}
}

func TestValidationWorkerRecoveryExhaustionIsIdempotent(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	started, deadline, lease := int64(900000), int64(1000000), int64(999999)
	repository.work.State = "RUNNING"
	repository.work.Attempt = 1
	repository.work.StartedMS, repository.work.DeadlineMS, repository.work.LeaseMS = &started, &deadline, &lease
	for range 2 {
		ids, err := worker.Recover(t.Context())
		if err != nil || len(ids) != 0 {
			t.Fatalf("recovery: %v %v", ids, err)
		}
	}
	if repository.work.State != "FAILED" || !reflect.DeepEqual(repository.events, []string{"FAILED"}) || *repository.work.DeadlineMS != deadline {
		t.Fatal("expired execution revived")
	}
}

func TestValidationWorkerRenewalFailureCancelsEvaluationAndJoins(t *testing.T) {
	worker, repository, ticker := newValidationTestWorker(t)
	cause := errors.New("heartbeat unavailable")
	repository.renewError = cause
	entered := make(chan struct{})
	repository.readFacts = func(ctx context.Context) (model.ValidationFacts, error) {
		close(entered)
		<-ctx.Done()
		return model.ValidationFacts{}, context.Cause(ctx)
	}
	result := make(chan error, 1)
	go func() { result <- worker.Run(t.Context(), "job") }()
	<-entered
	ticker.ticks <- worker.environment.Now()
	select {
	case err := <-result:
		validationTestCause(t, err, cause)
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not cancel evaluation")
	}
	select {
	case <-ticker.stopped:
	default:
		t.Fatal("ticker not stopped")
	}
	if repository.work.State != "FAILED" || !repository.terminal.Retryable {
		t.Fatal("heartbeat failure not retryable")
	}
}

func TestValidationWorkerHeartbeatRetainsDeadlineAndAttemptAuthority(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	claim, ok, err := worker.claim(t.Context(), "job")
	if err != nil || !ok {
		t.Fatalf("claim: %v/%v", ok, err)
	}
	deadline := *claim.Job.DeadlineMS
	worker.environment.Now = func() time.Time { return time.UnixMilli(deadline - 1000) }
	// A still-live final-second lease is capped to the original deadline.
	lease := deadline
	repository.work.LeaseMS = &lease
	if err := worker.heartbeat(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	if *repository.work.LeaseMS != deadline || repository.work.Attempt != 1 || *repository.work.DeadlineMS != deadline {
		t.Fatal("heartbeat reset execution budget")
	}
	if !ownsValidationWork(repository.work, claim, deadline-1) {
		t.Fatal("heartbeat version change revoked valid attempt")
	}
	repository.work.ExecutionNo++
	validationTestCause(t, worker.heartbeat(t.Context(), claim), model.ErrValidationOwnership)
}

func TestValidationWorkerCallerCancellationPreservesCause(t *testing.T) {
	worker, repository, ticker := newValidationTestWorker(t)
	cause := errors.New("server shutdown")
	ctx, cancel := context.WithCancelCause(t.Context())
	repository.readFacts = func(ctx context.Context) (model.ValidationFacts, error) {
		cancel(cause)
		return model.ValidationFacts{}, ctx.Err()
	}
	validationTestCause(t, worker.Run(ctx, "job"), cause)
	if repository.work.State != "FAILED" || !repository.terminal.Retryable {
		t.Fatal("cancelled attempt did not close retryably")
	}
	select {
	case <-ticker.stopped:
	default:
		t.Fatal("cancelled attempt leaked monitor")
	}
}

func TestValidationWorkerDoesNotGenerateIdentityForFutureWork(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	repository.work.AvailableMS = worker.environment.Now().UnixMilli() + 1
	calls := 0
	worker.environment.NewID = func() (string, error) { calls++; return "", errors.New("must not allocate") }
	if err := worker.Run(t.Context(), "job"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || repository.work.State != "QUEUED" || repository.work.Attempt != 0 {
		t.Fatal("future job acquired an owner")
	}
}

func TestValidationWorkerFinalVariantVersionRejectsManualChanges(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	before := repository.facts
	repository.readFacts = func(context.Context) (model.ValidationFacts, error) {
		repository.facts.VariantVersion++
		return before, nil
	}
	validationTestCause(t, worker.Run(t.Context(), "job"), ErrValidationGameChanged)
	if repository.variants != 0 || repository.work.State != "CANCELLED" {
		t.Fatal("manual variant change overwritten")
	}
}

func TestValidationWorkerFinalWriteRollsBackWithCompletionEvent(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	cause := errors.New("completion event unavailable")
	repository.finishError = cause
	validationTestCause(t, worker.Run(t.Context(), "job"), cause)
	if repository.variants != 0 || repository.work.State != "RUNNING" || !reflect.DeepEqual(repository.events, []string{"STARTED"}) {
		t.Fatal("completion event failure retained variant or terminal writes")
	}
}

func TestValidationWorkerExecutionAuthorityIncludesSnapshotAndScope(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	claim, ok, err := worker.claim(t.Context(), "job")
	if err != nil || !ok {
		t.Fatal("claim failed")
	}
	current := repository.work
	tests := []struct {
		name   string
		mutate func(*model.ValidationWork)
	}{
		{"execution", func(work *model.ValidationWork) { work.ExecutionNo++ }},
		{"attempt", func(work *model.ValidationWork) { work.Attempt++ }},
		{"snapshot", func(work *model.ValidationWork) { work.InputDigest = "changed" }},
		{"snapshot bytes", func(work *model.ValidationWork) { work.SnapshotJSON = "changed" }},
		{"kind", func(work *model.ValidationWork) { work.Kind = "IMPORT_GROUP" }},
		{"scope", func(work *model.ValidationWork) { work.ScopeID = "other variant" }},
		{"deadline", func(work *model.ValidationWork) { value := *work.DeadlineMS + 1; work.DeadlineMS = &value }},
		{"start", func(work *model.ValidationWork) { value := *work.StartedMS + 1; work.StartedMS = &value }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := current
			test.mutate(&changed)
			if ownsValidationWork(changed, claim, worker.environment.Now().UnixMilli()) {
				t.Fatal("accepted changed owner")
			}
		})
	}
}

func TestValidationWorkerOwnExpiredDeadlineClosesWithoutVariantWrite(t *testing.T) {
	worker, repository, _ := newValidationTestWorker(t)
	repository.readFacts = func(context.Context) (model.ValidationFacts, error) {
		deadline := *repository.work.DeadlineMS
		worker.environment.Now = func() time.Time { return time.UnixMilli(deadline) }
		return repository.facts, nil
	}
	validationTestCause(t, worker.Run(t.Context(), "job"), context.DeadlineExceeded)
	if repository.work.State != "FAILED" || !repository.terminal.Retryable || repository.variants != 0 {
		t.Fatal("expired owned attempt remained running or wrote variant")
	}
}
